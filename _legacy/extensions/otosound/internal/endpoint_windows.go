//go:build windows

package internal

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Asking Windows whether a machine has an output device at all.
//
// This is a second opinion on a question oto answers internally and then throws
// away. oto v3.5.0's driver_windows.go tries WASAPI, falls back to WinMM, and
// when both report its own errDeviceNotFound it installs a nullContext: a
// goroutine that drains the mux in real time forever, whose Err reports nil and
// which is unexported, so from outside the package a machine with no sound card
// is indistinguishable from a working one. An Adapter whose single promise is
// to say "there is no device" cannot keep it by asking oto.
//
// So the condition is evaluated here instead, and it is evaluated as oto's own
// fallback chain evaluates it rather than approximately:
//
//   - WinMM reports errDeviceNotFound when waveOutOpen on the wave mapper
//     answers ERROR_NOT_FOUND or MMSYSERR_BADDEVICEID, which is what it answers
//     when the machine has no output devices. waveOutGetNumDevs counts exactly
//     those devices and opens nothing, so it is the half of the conjunction
//     that can be asked without taking the device away from anyone.
//   - WASAPI reports errDeviceNotFound when IMMDeviceEnumerator's
//     GetDefaultAudioEndpoint for a render endpoint fails with a FACILITY_WIN32
//     HRESULT - E_NOTFOUND, in the case that matters.
//
// Both halves have to say no. Either one alone is a guess: a default endpoint
// with no WinMM devices, or WinMM devices with no default endpoint, are both
// machines where one of oto's two drivers still starts and the game is audible,
// and reporting ErrDeviceUnavailable on a machine that can make a sound is a
// worse answer than the bug this fixes.
//
// Everything below is conservative in the same direction. A probe that cannot
// be carried out - COM refuses to start, the enumerator cannot be created, the
// call fails with an HRESULT that is not a Win32 one - reports nothing, because
// "I could not find out" is not "there is no device".

var (
	ole32 = windows.NewLazySystemDLL("ole32.dll")
	winmm = windows.NewLazySystemDLL("winmm.dll")

	procCoCreateInstance  = ole32.NewProc("CoCreateInstance")
	procWaveOutGetNumDevs = winmm.NewProc("waveOutGetNumDevs")
)

var (
	clsidMMDeviceEnumerator = windows.GUID{
		Data1: 0xbcde0395, Data2: 0xe52f, Data3: 0x467c,
		Data4: [8]byte{0x8e, 0x3d, 0xc4, 0x57, 0x92, 0x91, 0x69, 0x2e},
	}
	iidIMMDeviceEnumerator = windows.GUID{
		Data1: 0xa95664d2, Data2: 0x9614, Data3: 0x4f35,
		Data4: [8]byte{0xa7, 0x46, 0xde, 0x8d, 0xb6, 0x36, 0x17, 0xe6},
	}
)

const (
	// clsctxAll is CLSCTX_ALL: in-process server, in-process handler, local
	// server, remote server. It is what oto passes for the same object.
	clsctxAll = 0x1 | 0x2 | 0x4 | 0x10
	// eRender is EDataFlow's render direction and eConsole is ERole's default
	// role, which together name "the device Windows would play a game through".
	eRender  = 0
	eConsole = 0
	// facilityWin32 is the HRESULT facility every Win32 error code carries once
	// it has been wrapped as an HRESULT. oto treats a GetDefaultAudioEndpoint
	// failure in this facility as its errDeviceNotFound, so this file has to
	// agree with it exactly or the two would disagree about which machines get
	// a null context.
	facilityWin32 = 0x80070000
	facilityMask  = 0xffff0000
)

// immDeviceEnumeratorVtbl is IMMDeviceEnumerator's method table. The three
// IUnknown entries come first, in their fixed order, and the interface's own
// five follow; only GetDefaultAudioEndpoint and Release are called, and the
// rest are named so the offsets are readable rather than counted.
type immDeviceEnumeratorVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr

	enumAudioEndpoints                     uintptr
	getDefaultAudioEndpoint                uintptr
	getDevice                              uintptr
	registerEndpointNotificationCallback   uintptr
	unregisterEndpointNotificationCallback uintptr
}

type immDeviceEnumerator struct{ vtbl *immDeviceEnumeratorVtbl }

// immDeviceVtbl is IMMDevice's, of which only Release is used: the endpoint is
// wanted for its existence and nothing else, so it is released the moment it is
// found.
type immDeviceVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr

	activate          uintptr
	openPropertyStore uintptr
	getId             uintptr
	getState          uintptr
}

type immDevice struct{ vtbl *immDeviceVtbl }

// noOutputDevice reports a machine with no audio output device at all, and nil
// for a machine that has one or for a probe that could not be carried out.
//
// The cheap half runs first and answers on every ordinary machine, so the COM
// apartment below is reached only where the answer is already close to yes.
func noOutputDevice() error {
	if waveOutDevices() > 0 {
		return nil
	}
	if defaultRenderEndpointExists() {
		return nil
	}
	return errNoOutputDevice
}

// waveOutDevices is waveOutGetNumDevs: how many output devices WinMM can see.
// It takes no arguments, returns no error and opens nothing.
func waveOutDevices() uint32 {
	count, _, _ := procWaveOutGetNumDevs.Call()
	return uint32(count)
}

// defaultRenderEndpointExists asks WASAPI for the default render endpoint and
// reports whether one came back. A failure that is not Win32-facility, and a
// probe that could not be run at all, both report true: this function's false
// is the assertion that there is nothing there, and it is made only when
// Windows said so in the words oto reads as errDeviceNotFound.
func defaultRenderEndpointExists() bool {
	exists := true
	onCOMApartment(func() {
		created := coCreateInstance(&clsidMMDeviceEnumerator, clsctxAll, &iidIMMDeviceEnumerator)
		if created == nil {
			return
		}
		enumerator := (*immDeviceEnumerator)(created)
		defer release(enumerator.vtbl.release, unsafe.Pointer(enumerator))

		var endpoint *immDevice
		hr, _, _ := syscall.SyscallN(enumerator.vtbl.getDefaultAudioEndpoint,
			uintptr(unsafe.Pointer(enumerator)), uintptr(eRender), uintptr(eConsole),
			uintptr(unsafe.Pointer(&endpoint)))
		if uint32(hr) == uint32(windows.S_OK) {
			release(endpoint.vtbl.release, unsafe.Pointer(endpoint))
			return
		}
		if uint32(hr)&facilityMask == facilityWin32 {
			exists = false
		}
	})
	return exists
}

// coCreateInstance is CoCreateInstance. A failure returns a nil pointer, which
// is the whole of what the caller acts on: an enumerator that cannot be created
// is a probe that did not happen, not an answer about a device.
func coCreateInstance(clsid *windows.GUID, context uint32, iid *windows.GUID) unsafe.Pointer {
	var created unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsid)), 0, uintptr(context),
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&created)))
	runtime.KeepAlive(clsid)
	runtime.KeepAlive(iid)
	if uint32(hr) != uint32(windows.S_OK) {
		return nil
	}
	return created
}

// release calls IUnknown::Release through a vtable slot.
func release(method uintptr, object unsafe.Pointer) {
	syscall.SyscallN(method, uintptr(object))
}

// onCOMApartment runs probe on a thread of its own with COM initialised, and
// does not return until it has finished.
//
// A thread of its own because COM is per thread and the caller's thread is not
// this package's to change: the open runs on the tick, a game's tick thread may
// already be in a single-threaded apartment, and initialising a different model
// on it would fail while uninitialising afterwards would tear down an apartment
// somebody else put there. A goroutine that locks a thread and never unlocks it
// leaves that thread to be destroyed when it returns, which takes the apartment
// with it.
//
// It costs a thread per probe, which is one at startup on a machine that has a
// device - waveOutGetNumDevs answers before this is reached - and one per retry
// on a machine that does not, at a cadence of one a second.
func onCOMApartment(probe func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.LockOSThread()
		// Deliberately no UnlockOSThread: see above.

		// S_FALSE is COM already being initialised on this thread in the same
		// model, which is a success and still owes a CoUninitialize.
		if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil &&
			!errors.Is(err, syscall.Errno(windows.S_FALSE)) {
			return
		}
		defer windows.CoUninitialize()
		probe()
	}()
	<-done
}
