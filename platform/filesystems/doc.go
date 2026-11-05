// Package filesystems holds the writable filesystems an application binds to
// platform's WriteFSSocket: NewDiskWriteFS keeps the files in a directory on
// disk, NewLocalStorageWriteFS, in a browser (js), in the page's
// localStorage. They are adapters, not plugins: the application's own plugin
// makes the one it wants, binds it in its Init and closes it in its Destroy,
// beside the read mounts it binds, any fs.FS, such as an embed.FS or an
// os.Root's FS.
//
//	func (p *game) Init(c *kernel.Configurator, _ any) error {
//		writable, err := filesystems.NewDiskWriteFS(userDirectory)
//		if err != nil {
//			return err
//		}
//		p.writable = writable
//		c.BindSocket[platform.WriteFSSocket](writable)
//		c.BindSocket(platform.ReadMountSocket{FS: assets})
//		return nil
//	}
//
// See ../docs/design.md.
package filesystems
