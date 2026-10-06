# Right-click entry in File Explorer

**Status on 6 October 2026:** a test package exists and works on the development PC, and
the same script can make the package for the Microsoft Store. Nothing was published.

Convert Me gets a **Convert with Convert Me** entry in the main right-click menu of File
Explorer on Windows 11. The entry belongs to the packaged version of the app. The
portable zip stays as it is: it adds nothing to File Explorer and it writes nothing to
the registry.

## Try the test package

You need Windows 11 or Windows 10 22H2 with **Developer Mode** switched on
(Settings > System > For developers), the Windows SDK, and the Microsoft C++ build tools
(Visual Studio 2022 or its Build Tools, with "Desktop development with C++").

```powershell
.\scripts\build.ps1                    # the app itself, as always
.\scripts\package-msix.ps1             # the command, the manifest, the logos: one folder
.\scripts\register-test-package.ps1    # tell Windows about that folder, for your user only
```

Then right-click a picture, an audio file or a video in File Explorer and choose
**Convert with Convert Me**. To take it out again:

```powershell
.\scripts\register-test-package.ps1 -Remove
```

What registering does, and what it does not do:

- It adds the right-click entry and a Start menu entry called "Convert Me Development".
- It needs no administrator rights, no certificate and no Store. It does not turn
  Developer Mode on.
- It copies nothing. Windows uses the folder `build\msix\development\layout` in place,
  so that folder has to stay where it is while the package is registered.
- It does not make Convert Me the app for any file type, and it does not add Convert Me
  to **Open with**.
- Removing it takes away both entries and the package's own data folder. Your settings
  for the portable app stay.

The test package and the Store version are two different packages for Windows, and each
brings its own menu entry. Remove the test package before you install the Store version.

## How it works

Windows 11 takes entries for its main menu only from an app that has a package identity,
and only through a small native DLL that is named in the package manifest. Paint, Photos,
Clipchamp and Notepad do it the same way.

1. **The command** is `shellext\ConvertMeCommand.cpp`, built as `ConvertMeCommand.dll`.
   It implements `IExplorerCommand`. Windows loads it in a helper process of its own
   (`dllhost.exe`), not inside File Explorer. It supplies the title and the icon, and it
   receives the whole selection in one call. It never opens a selected file and knows
   nothing about formats, so the menu is not slowed down.
2. **The hand-off.** The command writes the selected paths to a small list file in the
   temporary folder and starts `ConvertMe.exe` once, with `--files-from` and the name of
   that list. One start means there is no limit on the number of files and nothing to
   merge. The app reads the list and removes it (`launch.go`). It only reads, and only
   removes, a file whose name and first line both say that it is such a list.
3. **The manifest** is made from `packaging\msix\AppxManifest.xml.in`. It names the DLL
   as a COM server (`com:SurrogateServer`) and lists the file types the command is shown
   for (`desktop4:FileExplorerContextMenus`). `scripts\package-msix.ps1` fills in the
   identity of the package: a development identity for the test package, or the one
   from Partner Center for the Store.
4. **The file types** are the types the app can read (`internal\convert\formats.go`),
   without `.ts` and `.mts`. Those two are far more often TypeScript source than video.
   The app still accepts them when they are dropped on its window. `packaging_test.go`
   fails when the manifest and the app disagree.
5. **An open window is reused.** If Convert Me is already open, the files go to that
   window. That also works when the open window is the portable app.

`scripts\build-shell-extension.ps1` builds the DLL and tests it without registering
anything: what the command says about itself, what it hands over for file names with
spaces, signs and letters outside ASCII, and that the app reads exactly that list.

## What was checked on the development PC

Windows 11 Enterprise 25H2, with the test package registered:

- The entry is in the main menu for a `.png` file, not under "Show more options".
  Choosing it opens Convert Me with that file.
- For a selection of five files of mixed types, File Explorer offers the command and
  hands all five over in one call. The four that Convert Me reads are listed. The text
  file among them is skipped with a message.
- The command is not offered for a `.txt` file.
- The app runs with the identity of the package, converts files, and leaves no list file
  behind.
- With the portable app already open, the files arrive in that open window.

Good to know: after a pause, Windows has to start its helper process for the command
before the command can answer. In one test that asked File Explorer for its list of
commands from a script, the very first answer came back without the command, and the
next one had it. If the entry is ever missing on a first right-click, right-click again.

Not checked yet:

- That the Convert Me window comes in front of other windows. The PC was locked while
  the tests ran, so there was no "in front" to look at.
- A selection of several hundred files through the menu by hand. The list is built for
  it: the command stops at 5000 paths, and the app's own list holds 500 files.
- Windows 10, where the same entry should show in the classic menu.
- An ARM device. It needs an ARM64 build of the DLL.

## The Store package

`scripts\package-msix.ps1 -StoreIdentity <file>` makes the package for the Microsoft
Store. README.md says what is in that file. Compared with the test package it has the
name, the publisher and the identity that Partner Center assigned, and nothing else is
different: the same app, the same command, the same file types.

Done for both packages:

- A full set of logos with a `resources.pri`, so the Start menu and the taskbar show the
  icon at every size.
- The package version: the app version with 1 added to its first number, and 0 as the
  fourth number.
- The source of the engine inside the package, in its `source` folder.
- One line in the notices for the Microsoft C++ runtime, which is linked into the DLL.

Left out on purpose, or still open:

- File type associations, so Convert Me is offered under **Open with**. They were left
  out, because Windows can ask "which app should open this?" again after a new app
  claims a file type. With them, declare `MultiSelectModel="Player"`.
- An ARM64 build of the DLL next to the x64 one. The package is x64 only.
- Uploading the package and sending it in for certification. That is done by hand in
  Partner Center, by the owner of the account.

One known limit of the app that the package does not touch: when Convert Me is started
many times at the same moment (thirty starts in a test), a second window can open. The
menu command starts the app once for a whole selection, so it cannot cause that.

## Why not a registry entry

A per-user registry entry is the classic way and needs no admin rights. It was
considered on 2 October 2026 and not chosen:

- On Windows 11 it only shows under **Show more options**.
- Windows starts the app once for every selected file, for up to 100 files, and the app
  has to merge those starts into one list.
- If the app folder is deleted while the entry is switched on, a dead menu entry stays
  behind.
- It does not work from a Store package. Windows keeps the registry writes of such a
  package in a private copy that File Explorer never reads.

The earlier app in this repository used the registry way. Its code is on the branch
`backup/explorer-image-converter-2026-08-21`, in `shell_windows.go` and
`explorer_coalesce_windows.go`.

## Microsoft documentation this is based on

- [Add a File Explorer context menu command to a packaged desktop app](https://learn.microsoft.com/windows/apps/desktop/modernize/integrate-packaged-app-with-file-explorer)
- [Extending the Context Menu and Share Dialog in Windows 11](https://blogs.windows.com/windowsdeveloper/2021/07/19/extending-the-context-menu-and-share-dialog-in-windows-11/)
- [Choosing a Static or Dynamic Shortcut Menu Method](https://learn.microsoft.com/windows/win32/shell/shortcut-choose-method)
- [How to Employ the Verb Selection Model](https://learn.microsoft.com/windows/win32/shell/how-to-employ-the-verb-selection-model)
- [Grant package identity by packaging with external location](https://learn.microsoft.com/windows/apps/desktop/modernize/grant-identity-to-nonpackaged-apps)
- [Understanding how packaged desktop apps run on Windows](https://learn.microsoft.com/windows/msix/desktop/desktop-to-uwp-behind-the-scenes)
- [Integrate your desktop app with Windows using packaging extensions](https://learn.microsoft.com/windows/apps/desktop/modernize/desktop-to-uwp-extensions)
