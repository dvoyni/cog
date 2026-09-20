//go:build js

package config

import "syscall/js"

// readCogEnv reads the page's COG_ENV. localStorage is the one place an
// override can be put on the browser and still be there after a reload, which
// is what makes it settable once from a console.
//
// Every step is guarded: localStorage is absent under file:// and the property
// itself throws when site data is blocked, and a page that cannot be
// configured must still run.
func readCogEnv() (packed string) {
	defer func() {
		if recover() != nil {
			packed = ""
		}
	}()
	storage := js.Global().Get("localStorage")
	if !storage.Truthy() {
		return ""
	}
	if item := storage.Call("getItem", cogEnvKey); item.Type() == js.TypeString {
		return item.String()
	}
	return ""
}
