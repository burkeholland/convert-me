# Right-click entry in File Explorer: plan for the packaged version

**Status:** decided on 2 October 2026. Not built yet.

Convert Me will get a **Convert with Convert Me** entry in the main right-click menu of
File Explorer on Windows 11. It will arrive with the packaged (MSIX, Microsoft Store)
version of the app. The portable zip stays as it is: it adds nothing to File Explorer and
it writes nothing to the registry.

This note records what was decided and why, so the work can start from here later.

## What was decided

| Question | Answer |
| --- | --- |
| Where does the entry go? | The main Windows 11 menu, not "Show more options" |
| Which version gets it? | The packaged version only |
| Does the portable zip get a registry switch? | No |
| What is approved today? | Only this note. Packaging, registering a test package, signing and Store steps each need the owner's go-ahead |

## Why not a registry entry

A per-user registry entry is the classic way and needs no admin rights. It was considered
and not chosen, for these reasons:

- On Windows 11 it only shows under **Show more options**. Windows keeps the main menu for
  apps that have a package identity.
- Windows starts the app once for every selected file, for up to 100 files, and the app has
  to merge those starts into one list. A test with 30 files and version 0.1.0 lost no file,
  but it opened two windows.
- If the app folder is deleted while the entry is switched on, a dead menu entry stays
  behind.
- It does not work from a Store package. Windows keeps the registry writes of such a
  package in a private copy that File Explorer never reads.

## How the packaged version will do it

Windows 11 takes main-menu entries only from an app with a package identity, through a
small native DLL that is listed in the package manifest. Paint, Photos, Clipchamp and
Notepad work the same way.

1. **A small native DLL** that implements `IExplorerCommand`. It supplies the title and
   the icon, hides the entry when nothing in the selection can be converted (`GetState`),
   and receives the whole selection in one call (`Invoke`). Windows only loads this kind
   of command from a DLL, so it cannot live in `ConvertMe.exe`. File Explorer waits for
   the DLL while it builds the menu, so the DLL must be fast and do as little as possible.
2. **Hand-off to the app.** The DLL writes the selected paths to a list file and starts
   `ConvertMe.exe` once. One start means there is nothing to merge and no limit on the
   number of files. The app needs a new argument for that list. Today it ignores every
   argument that starts with `-`.
3. **Manifest entries.**
   - `com:SurrogateServer` with the class of the DLL.
   - `desktop4:FileExplorerContextMenus` with one `desktop5:ItemType` for each file
     extension the app can read (the list in `internal/convert/formats.go`), each with a
     `desktop5:Verb` that points at that class. The entry then only shows on files
     Convert Me can read.
   - File type associations for the same extensions, so Convert Me is also offered under
     **Open with**.
4. **Architecture.** The DLL has to match File Explorer. An x64 DLL covers x64 PCs. ARM
   devices need an ARM64 DLL as well.
5. **Install and removal.** Windows adds the entry when the package is installed and
   removes it when the package is uninstalled. No admin rights, and no registry code in
   the app.

## Before building it

- Check first, with a test package, whether a manifest-only entry reaches the main menu.
  That would be a verb on the file type association, without a DLL. Microsoft's
  documentation only promises **Open with** and edit-style entries for it, so the expected
  answer is no. If the answer is yes, the DLL is not needed.
- Close the gap that the 30 file test showed: when the app is started many times at the
  same moment, a second window can open. The file type associations in the package should
  also declare `MultiSelectModel="Player"`, so that Windows passes all files in one start.
- Testing needs a development package registered on the test PC. Signing and the Store
  submission are the owner's steps.

## For reference

The earlier app in this repository used the registry way. Its code is on the branch
`backup/explorer-image-converter-2026-08-21`, in `shell_windows.go` and
`explorer_coalesce_windows.go`.

Microsoft documentation this plan is based on, read on 2 October 2026:

- [Add a File Explorer context menu command to a packaged desktop app](https://learn.microsoft.com/windows/apps/desktop/modernize/integrate-packaged-app-with-file-explorer)
- [Extending the Context Menu and Share Dialog in Windows 11](https://blogs.windows.com/windowsdeveloper/2021/07/19/extending-the-context-menu-and-share-dialog-in-windows-11/)
- [Choosing a Static or Dynamic Shortcut Menu Method](https://learn.microsoft.com/windows/win32/shell/shortcut-choose-method)
- [How to Employ the Verb Selection Model](https://learn.microsoft.com/windows/win32/shell/how-to-employ-the-verb-selection-model)
- [Grant package identity by packaging with external location](https://learn.microsoft.com/windows/apps/desktop/modernize/grant-identity-to-nonpackaged-apps)
- [Understanding how packaged desktop apps run on Windows](https://learn.microsoft.com/windows/msix/desktop/desktop-to-uwp-behind-the-scenes)
- [Integrate your desktop app with Windows using packaging extensions](https://learn.microsoft.com/windows/apps/desktop/modernize/desktop-to-uwp-extensions)
