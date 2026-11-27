//go:build windows

package otosound

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Asking Windows whether the machine has an output device at all.
//
// It is a second opinion on a question oto answers internally and then
// throws away. oto v3.5.1's Windows driver tries WASAPI, falls back to WinMM,
// and when both report its own errDeviceNotFound it installs a null context:
// a goroutine that drains the mux in real time forever, whose Err reports nil
// and which is unexported, so from outside oto a machine with no sound card
// is indistinguishable from a working one. A backend whose one promise is to
// say "there is no device" cannot keep it by asking oto.
//
// So the question is asked here, the way oto's own fallback chain asks it,
// with both halves having to say no:
//
//   - WinMM reports errDeviceNotFound when waveOutOpen on the wave mapper
//     answers MMSYSERR_BADDEVICEID, which it does when the machine has no
//     output devices. waveOutGetNumDevs counts exactly those devices and
//     opens nothing, so it is the half that can be asked without taking the
//     device from anyone.
//   - WASAPI reports errDeviceNotFound when IMMDeviceEnumerator's
//     GetDefaultAudioEndpoint for a render endpoint fails with a
//     FACILITY_WIN32 HRESULT: E_NOTFOUND, in the case that matters.
//
// Either half alone is a guess: a default endpoint with no WinMM devices, or
// WinMM devices with no default endpoint, are both machines where one of
// oto's drivers still starts and the game is audible, and a false "absent"
// on a machine that can make a sound is a worse answer than the bug the
// probe fixes. For the same reason a probe that cannot be carried out, COM
// refusing to start, the enumerator not made, the call failing with an
// HRESULT that is not a Win32 one, reports nothing: "I could not find out"
// is not "there is no device".

var (
	ole32 = windows.NewLazySystemDLL("ole32.dll")
	winmm = windows.NewLazySystemDLL("winmm.dll")

	procCoCreateInstance  = ole32.NewProc("CoCreateInstance")
	procWaveOutGetNumDevs = winmm.NewProc("waveOutGetNumDevs")

	clsidMMDeviceEnumerator = windows.GUID{
		Data1: 0xbcde0395, Data2: 0xe52f, Data3: 0x467c,
		Data4: [8]byte{0x8e, 0x3d, 0xc4, 0x57, 0x92, 0x91, 0x69, 0x2e},
	}
	iidIMMDeviceEnumerator = windows.GUID{
		Data1: 0xa95664d2, Data2: 0x9614, Data3: 0x4f35,
		Data4: [8]byte{0xa7, 0x46, 0xde, 0x8d, 0xb6, 0x36, 0x17, 0xe6},
	}
)

// The values the probe passes to and reads from COM.
const (
	// classContextAll is CLSCTX_ALL: an in-process server or handler, a local
	// server or a remote one. It is what oto passes for the same object.
	classContextAll = 0x1 | 0x2 | 0x4 | 0x10
	// dataFlowRender is EDataFlow's eRender and roleConsole ERole's eConsole,
	// which together name the device Windows would play a game through.
	dataFlowRender = 0
	roleConsole    = 0
	// facilityWin32 is the facility every Win32 error code carries once it is
	// wrapped as an HRESULT. oto takes a GetDefaultAudioEndpoint failure in
	// it for its errDeviceNotFound, so the probe agrees with oto exactly
	// about which machines get a null context.
	facilityWin32 = 0x80070000
	facilityMask  = 0xffff0000
)

// immDeviceEnumeratorVTable is IMMDeviceEnumerator's method table: IUnknown's
// three entries in their fixed order, then the interface's own five. Only
// GetDefaultAudioEndpoint and Release are called; the rest are named so the
// offsets are read rather than counted.
type immDeviceEnumeratorVTable struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr

	enumAudioEndpoints                     uintptr
	getDefaultAudioEndpoint                uintptr
	getDevice                              uintptr
	registerEndpointNotificationCallback   uintptr
	unregisterEndpointNotificationCallback uintptr
}

// immDeviceEnumerator is an IMMDeviceEnumerator as COM lays it out.
type immDeviceEnumerator struct {
	vTable *immDeviceEnumeratorVTable
}

// immDeviceVTable is IMMDevice's method table, of which only Release is
// called: the endpoint is wanted for its existence alone, so it is released
// the moment it is found.
type immDeviceVTable struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr

	activate          uintptr
	openPropertyStore uintptr
	getID             uintptr
	getState          uintptr
}

// immDevice is an IMMDevice as COM lays it out.
type immDevice struct {
	vTable *immDeviceVTable
}

// probeOutputDevice answers errNoOutputDevice on a machine with no audio
// output device at all, and nil on one that has one or when the probe could
// not be carried out. The cheap half runs first and answers on every
// ordinary machine, so COM is reached only where WinMM counts no devices.
func probeOutputDevice() error {
	// waveOutGetNumDevs counts the output devices WinMM sees. It takes no
	// arguments, answers no error and opens nothing.
	if waveOutDevices, _, _ := procWaveOutGetNumDevs.Call(); uint32(waveOutDevices) > 0 || probeDefaultRenderEndpoint() {
		return nil
	}
	return errNoOutputDevice
}

// probeDefaultRenderEndpoint asks WASAPI for the default render endpoint
// and answers whether one came back. A failure that is not a Win32 one, and a
// probe that could not run at all, both answer true: false asserts there is
// nothing there, and is answered only when Windows said so in the words oto
// reads as errDeviceNotFound.
func probeDefaultRenderEndpoint() bool {
	exists := true
	runOnCOMApartment(func() {
		created := createCOMInstance(&clsidMMDeviceEnumerator, classContextAll, &iidIMMDeviceEnumerator)
		if created == nil {
			return
		}
		enumerator := (*immDeviceEnumerator)(created)
		// Each object found is released through IUnknown::Release, its
		// method table's third entry.
		defer syscall.SyscallN(enumerator.vTable.release, uintptr(unsafe.Pointer(enumerator)))

		var endpoint *immDevice
		result, _, _ := syscall.SyscallN(enumerator.vTable.getDefaultAudioEndpoint,
			uintptr(unsafe.Pointer(enumerator)), dataFlowRender, roleConsole,
			uintptr(unsafe.Pointer(&endpoint)))
		if uint32(result) == uint32(windows.S_OK) {
			syscall.SyscallN(endpoint.vTable.release, uintptr(unsafe.Pointer(endpoint)))
			return
		}
		if uint32(result)&facilityMask == facilityWin32 {
			exists = false
		}
	})
	return exists
}

// createCOMInstance is CoCreateInstance. A failure answers nil, which is all
// its caller acts on: an enumerator that cannot be made is a probe that did
// not happen, not an answer about a device.
func createCOMInstance(classID *windows.GUID, classContext uint32, interfaceID *windows.GUID) unsafe.Pointer {
	var created unsafe.Pointer
	result, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(classID)), 0, uintptr(classContext),
		uintptr(unsafe.Pointer(interfaceID)), uintptr(unsafe.Pointer(&created)))
	if uint32(result) != uint32(windows.S_OK) {
		return nil
	}
	return created
}

// runOnCOMApartment runs probe on a thread of its own with COM initialized,
// and returns once it has finished.
//
// A thread of its own because COM is per thread, and the thread Voices runs
// on is not otosound's to change: it may already be in an apartment of
// another model, where initializing this one would fail and uninitializing
// after would tear down an apartment someone else put there. A goroutine
// that locks its thread and never unlocks it has the thread ended when it
// returns, which takes the apartment with it. It costs a thread per probe,
// and is reached only on a machine where WinMM counts no devices: once a
// retry on such a machine.
func runOnCOMApartment(probe func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		// No UnlockOSThread, so the thread ends with the goroutine.

		// S_FALSE is COM already initialized on this thread in the same
		// model, which is a success and still owes a CoUninitialize.
		if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil && !errors.Is(err, syscall.Errno(windows.S_FALSE)) {
			return
		}
		defer windows.CoUninitialize()
		probe()
	}()
	<-done
}
