# Privacy

Convert Me converts your files on your own PC. It has no account, no analytics, no
telemetry, no update check and no cloud service. The app itself makes no network requests, and
its conversion engine is built without any network code, so it could not send a file
anywhere even if it were asked to.

## What the app reads

Only the files you put in the list. It reads each one to find out what it is, to make
the small preview picture in the list, and to convert it. Your originals are opened for
reading only. They are never changed, moved or deleted.

HEIC photos are read by Windows for the app, with the HEIC codecs from Microsoft that
are installed on the PC. That happens on your PC, like everything else.

## What the app writes

- **The converted files**, in the folder you chose: next to each original, or one folder
  you picked. While a file is being converted it has a temporary name ending in
  `.convertme-part` in that same folder. It is removed if you cancel or close the app.
- **Your settings**, in `%LOCALAPPDATA%\ConvertMe\settings.json`: the format you last
  chose for images, for audio and for video, and the folder you last chose to save to.
  That is all. No file names, no list of what you converted, no history.
- **Short-lived work files**, in `%LOCALAPPDATA%\ConvertMe\work`: a color table while a
  video is being turned into a GIF, and a copy of the picture while a HEIC photo is
  being listed or converted. Each is deleted as soon as that step ends, and the folder
  is emptied each time the app starts.
- **A list of the files you selected**, only in the packaged version of the app (the one
  from the Microsoft Store), and only when you use its **Convert with Convert Me**
  command in File Explorer. The command writes the names of the selected files to a
  small file in your temporary folder, so that the app can read them. The app removes
  that file as soon as it has read it.

The preview pictures in the list are kept in memory only. They are never written to disk.

The interface is drawn by Microsoft Edge WebView2, a part of Windows. It keeps its own
profile folder in `%APPDATA%\ConvertMe.exe`. Convert Me stores one thing there: whether
you chose the light or the dark theme.

To remove every trace of the portable app, delete its folder and those two folders.

**The version from the Microsoft Store** stores the same things and nothing more. On a
PC where the portable app was never used, Windows keeps both folders inside the app's
own storage area, under `%LOCALAPPDATA%\Packages`, and deletes them when you uninstall
the app. On a PC where the portable app was used before, the Store version goes on
using the two folders named above, and they stay when you uninstall it.

## What stays inside a converted file

Converting is not the same as cleaning. Details that are stored inside your original can
also end up inside the new file:

- Music keeps its tags (title, artist, album) and its cover picture where the new format
  can hold them.
- A PNG made from a photo keeps the photo's EXIF details. Those can include the camera,
  the date, and the place where the photo was taken. Other picture formats made by
  Convert Me do not carry EXIF details. Nothing made from a HEIC photo carries them,
  PNG included.
- Video keeps its title and, when the original has one, its location tag.
- The engine writes its own name and version into audio and video files it makes.

Convert Me does not add anything about you, your PC or your other files. If you plan to
share a file and its location matters, check the result with a tool made for removing
metadata.

## The internet

Converting needs no internet connection at all.

One Windows component may connect on its own: if Microsoft Edge WebView2 is missing, the
Microsoft installer that is part of the app downloads it once. Its installation and its
updates are covered by Microsoft's terms and privacy statement, not by this document.
Windows 11 and up-to-date Windows 10 already have it.

If you get the app from the Microsoft Store, Windows downloads, installs and updates it.
That is done by the Store, not by Convert Me, and Microsoft's privacy statement covers it.

Building the app from source downloads source code and build tools. None of that
involves your media files.

## Who can see your files

Converted files and settings are ordinary files in your Windows user profile. They are
protected by your Windows account and by disk encryption if you use it, not by the app.
Anyone who can open your profile can open them.

## Use it responsibly

Only convert files you are allowed to use. Convert Me does not remove copy protection.
