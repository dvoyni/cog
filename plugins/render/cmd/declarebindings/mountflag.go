package main

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/dvoyni/cog/plugins/render/cmd/declarebindings/plugin"
)

// mountFlag is the folders the -mount flags mount, one for each.
type mountFlag []plugin.Mount

// String answers the mounts as -mount spells them, separated by spaces.
func (f *mountFlag) String() string {
	var spelt []string
	for _, mount := range *f {
		spelt = append(spelt, mount.Prefix+"="+mount.Directory)
	}
	return strings.Join(spelt, " ")
}

// Set adds the mount text spells, PREFIX=DIRECTORY: a prefix that is no valid
// path below the root, as platform refuses, or no directory, is an error.
func (f *mountFlag) Set(text string) error {
	prefix, directory, found := strings.Cut(text, "=")
	switch {
	case !found || directory == "":
		return fmt.Errorf("the mount %q names no folder, want PREFIX=DIRECTORY", text)
	case !fs.ValidPath(prefix) || prefix == ".":
		return fmt.Errorf("the mount %q has a prefix that is no valid path below the root", text)
	}
	*f = append(*f, plugin.Mount{Prefix: prefix, Directory: directory})
	return nil
}
