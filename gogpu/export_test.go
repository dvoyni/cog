package gogpu

import (
	"github.com/dvoyni/cog/platform"
	"github.com/gogpu/gpucontext"
)

// RequestClose runs the close callback of window's gogpu window, as the
// operating system does at the user's close, and answers what it answers:
// true closes the window.
func RequestClose(p *Plugin, window platform.Window) bool {
	return windowSystem{plugin: p}.requestClose(window)
}

// ReportPointer reports event in window, as the plugin's pointer callback on
// window's gogpu window does when the operating system calls it.
func ReportPointer(p *Plugin, window platform.Window, event gpucontext.PointerEvent) {
	inputSource{plugin: p}.reportPointer(window, event)
}

// ReportKey reports key pressed in window, or released when pressed is false,
// with modifiers, as the plugin's key callbacks on window's gogpu window do
// when the operating system calls them.
func ReportKey(p *Plugin, window platform.Window, key gpucontext.Key, modifiers gpucontext.Modifiers, pressed bool) {
	inputSource{plugin: p}.reportKey(window, key, modifiers, pressed)
}

// ReportText reports text typed in window, as the plugin's text callback on
// window's gogpu window does when the operating system calls it.
func ReportText(p *Plugin, window platform.Window, text string) {
	inputSource{plugin: p}.reportText(window, text)
}

// ReportScroll reports event's scroll over window, as the plugin's scroll
// callback on window's gogpu window does when the operating system calls it.
func ReportScroll(p *Plugin, window platform.Window, event gpucontext.ScrollEvent) {
	inputSource{plugin: p}.reportScroll(window, event)
}

// ReportFocus reports window's gain of the focus, or its loss when focused is
// false, as the plugin's focus callback on window's gogpu window does when
// the operating system calls it.
func ReportFocus(p *Plugin, window platform.Window, focused bool) {
	windowSystem{plugin: p}.reportFocus(window, focused)
}

// MarkDeviceLoss answers err as the device's acquire, present and submit
// answer it.
func MarkDeviceLoss(err error) error { return markDeviceLoss(err) }
