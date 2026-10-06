// The "Convert with Convert Me" command in the File Explorer menu of the packaged version.
//
// Windows loads this DLL in a helper process of its own (dllhost.exe), asks it for the
// title and the icon, and calls Invoke with the selected files. Invoke writes their paths
// to a list file and starts ConvertMe.exe once. The app reads the list and removes it
// (launch.go).
//
// File Explorer waits for these answers while it builds the menu. So nothing here opens a
// selected file, and nothing here knows which formats the app supports. The package
// manifest decides for which file types the command is shown.

#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <shlobj.h>
#include <shlwapi.h>

#include <new>
#include <string>
#include <vector>

#include "clsid.h"

namespace {

// These four values are a contract with launch.go.
constexpr wchar_t kListFlag[] = L"--files-from";
constexpr char kListHeader[] = "Convert Me file list 1\n";
constexpr wchar_t kListNamePrefix[] = L"convertme-files-";
constexpr wchar_t kListNameSuffix[] = L".txt";

// The app reads at most this many paths, and its list holds fewer still. Stopping here
// keeps the list file far below the size the app accepts.
constexpr size_t kMaxPaths = 5000;
constexpr size_t kMaxListBytes = 4 * 1024 * 1024;

constexpr wchar_t kTitle[] = L"Convert with Convert Me";
constexpr wchar_t kAppName[] = L"ConvertMe.exe";

HINSTANCE g_module = nullptr;
LONG g_objects = 0;
LONG g_locks = 0;
LONG g_listNumber = 0;

// The folder this DLL is in. ConvertMe.exe is next to it.
std::wstring ModuleFolder() {
    std::wstring path(MAX_PATH, L'\0');
    for (;;) {
        const DWORD length = GetModuleFileNameW(g_module, path.data(), static_cast<DWORD>(path.size()));
        if (length == 0) return {};
        if (length < path.size()) {
            path.resize(length);
            break;
        }
        path.resize(path.size() * 2);
    }
    const size_t slash = path.find_last_of(L'\\');
    return slash == std::wstring::npos ? std::wstring() : path.substr(0, slash);
}

std::vector<std::wstring> SelectedPaths(IShellItemArray* items) {
    std::vector<std::wstring> paths;
    DWORD count = 0;
    if (!items || FAILED(items->GetCount(&count))) return paths;
    for (DWORD i = 0; i < count && paths.size() < kMaxPaths; i++) {
        IShellItem* item = nullptr;
        if (FAILED(items->GetItemAt(i, &item))) continue;
        PWSTR name = nullptr;
        // Only real files and folders have a file system path. Anything else is skipped.
        if (SUCCEEDED(item->GetDisplayName(SIGDN_FILESYSPATH, &name)) && name) {
            paths.emplace_back(name);
            CoTaskMemFree(name);
        }
        item->Release();
    }
    return paths;
}

// One path as a UTF-8 line. Empty when the path cannot be written as one line of UTF-8.
std::string ListLine(const std::wstring& path) {
    if (path.empty() || path.find_first_of(L"\r\n") != std::wstring::npos) return {};
    const int wide = static_cast<int>(path.size());
    const int size = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, path.data(), wide, nullptr, 0, nullptr, nullptr);
    if (size <= 0) return {};
    std::string line(static_cast<size_t>(size), '\0');
    WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, path.data(), wide, line.data(), size, nullptr, nullptr);
    line.push_back('\n');
    return line;
}

// The path of a file as Windows itself knows it. Inside a package, a new file in the
// temporary folder can be stored somewhere else than its name says. This is the name
// that works from every process.
std::wstring FinalPath(HANDLE file, const std::wstring& fallback) {
    std::wstring path(MAX_PATH, L'\0');
    DWORD length = GetFinalPathNameByHandleW(file, path.data(), static_cast<DWORD>(path.size()), FILE_NAME_NORMALIZED | VOLUME_NAME_DOS);
    if (length >= path.size()) {
        path.resize(length + 1);
        length = GetFinalPathNameByHandleW(file, path.data(), static_cast<DWORD>(path.size()), FILE_NAME_NORMALIZED | VOLUME_NAME_DOS);
    }
    if (length == 0 || length >= path.size()) return fallback;
    path.resize(length);
    // "\\?\C:\folder\file" is the long form of "C:\folder\file".
    if (path.size() < MAX_PATH && path.compare(0, 4, L"\\\\?\\") == 0 && path.size() > 6 && path[5] == L':') {
        path.erase(0, 4);
    }
    return path;
}

// Writes the list file and returns its path, or an empty string.
std::wstring WriteList(const std::vector<std::wstring>& paths) {
    std::string content(kListHeader);
    size_t written = 0;
    for (const std::wstring& path : paths) {
        const std::string line = ListLine(path);
        if (line.empty()) continue;
        if (content.size() + line.size() > kMaxListBytes) break;
        content += line;
        written++;
    }
    if (written == 0) return {};

    wchar_t folder[MAX_PATH + 2];
    const DWORD folderLength = GetTempPathW(ARRAYSIZE(folder), folder);
    if (folderLength == 0 || folderLength >= ARRAYSIZE(folder)) return {};

    for (int attempt = 0; attempt < 8; attempt++) {
        const std::wstring name = std::wstring(folder) + kListNamePrefix +
            std::to_wstring(GetCurrentProcessId()) + L"-" + std::to_wstring(GetTickCount64()) + L"-" +
            std::to_wstring(InterlockedIncrement(&g_listNumber)) + kListNameSuffix;
        // CREATE_NEW never opens a file that is already there.
        const HANDLE file = CreateFileW(name.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_NEW, FILE_ATTRIBUTE_NORMAL, nullptr);
        if (file == INVALID_HANDLE_VALUE) {
            if (GetLastError() == ERROR_FILE_EXISTS) continue;
            return {};
        }
        bool ok = true;
        for (size_t offset = 0; ok && offset < content.size();) {
            DWORD count = 0;
            ok = WriteFile(file, content.data() + offset, static_cast<DWORD>(content.size() - offset), &count, nullptr) && count > 0;
            offset += count;
        }
        const std::wstring path = ok ? FinalPath(file, name) : std::wstring();
        CloseHandle(file);
        if (!ok) DeleteFileW(name.c_str());
        return path;
    }
    return {};
}

HRESULT StartApp(const std::wstring& list) {
    const std::wstring folder = ModuleFolder();
    if (folder.empty()) return E_FAIL;
    const std::wstring app = folder + L"\\" + kAppName;
    // The program is named separately, so Windows never has to guess where its path ends.
    // File names cannot contain a quotation mark, so the quoting below is complete.
    std::wstring command = L"\"" + app + L"\" " + kListFlag + L" \"" + list + L"\"";
    STARTUPINFOW startup{};
    startup.cb = sizeof(startup);
    PROCESS_INFORMATION process{};
    if (!CreateProcessW(app.c_str(), command.data(), nullptr, nullptr, FALSE, 0, nullptr, nullptr, &startup, &process)) {
        return HRESULT_FROM_WIN32(GetLastError());
    }
    // The user clicked in File Explorer, so the window that opens belongs in front.
    AllowSetForegroundWindow(process.dwProcessId);
    CloseHandle(process.hThread);
    CloseHandle(process.hProcess);
    return S_OK;
}

class __declspec(uuid(CONVERTME_COMMAND_CLSID)) ConvertCommand final : public IExplorerCommand {
public:
    ConvertCommand() { InterlockedIncrement(&g_objects); }

    IFACEMETHODIMP QueryInterface(REFIID id, void** object) override {
        if (!object) return E_POINTER;
        if (id == IID_IUnknown || id == __uuidof(IExplorerCommand)) {
            *object = static_cast<IExplorerCommand*>(this);
            AddRef();
            return S_OK;
        }
        *object = nullptr;
        return E_NOINTERFACE;
    }
    IFACEMETHODIMP_(ULONG) AddRef() override { return InterlockedIncrement(&references_); }
    IFACEMETHODIMP_(ULONG) Release() override {
        const ULONG left = InterlockedDecrement(&references_);
        if (left == 0) delete this;
        return left;
    }

    IFACEMETHODIMP GetTitle(IShellItemArray*, PWSTR* title) override {
        if (!title) return E_POINTER;
        return SHStrDupW(kTitle, title);
    }

    IFACEMETHODIMP GetIcon(IShellItemArray*, PWSTR* icon) override try {
        if (!icon) return E_POINTER;
        *icon = nullptr;
        const std::wstring folder = ModuleFolder();
        if (folder.empty()) return E_FAIL;
        // The first icon of the app itself.
        return SHStrDupW((folder + L"\\" + kAppName + L",0").c_str(), icon);
    } catch (...) {
        return E_OUTOFMEMORY;
    }

    IFACEMETHODIMP GetToolTip(IShellItemArray*, PWSTR* tip) override {
        if (tip) *tip = nullptr;
        return E_NOTIMPL;
    }

    IFACEMETHODIMP GetCanonicalName(GUID* name) override {
        if (!name) return E_POINTER;
        *name = GUID_NULL;
        return S_OK;
    }

    IFACEMETHODIMP GetState(IShellItemArray*, BOOL, EXPCMDSTATE* state) override {
        if (!state) return E_POINTER;
        *state = ECS_ENABLED;
        return S_OK;
    }

    IFACEMETHODIMP Invoke(IShellItemArray* items, IBindCtx*) override try {
        const std::vector<std::wstring> paths = SelectedPaths(items);
        if (paths.empty()) return S_OK;
        const std::wstring list = WriteList(paths);
        if (list.empty()) return E_FAIL;
        const HRESULT result = StartApp(list);
        if (FAILED(result)) DeleteFileW(list.c_str());
        return result;
    } catch (...) {
        return E_OUTOFMEMORY;
    }

    IFACEMETHODIMP GetFlags(EXPCMDFLAGS* flags) override {
        if (!flags) return E_POINTER;
        *flags = ECF_DEFAULT;
        return S_OK;
    }

    IFACEMETHODIMP EnumSubCommands(IEnumExplorerCommand** commands) override {
        if (commands) *commands = nullptr;
        return E_NOTIMPL;
    }

private:
    ~ConvertCommand() { InterlockedDecrement(&g_objects); }
    LONG references_ = 1;
};

class CommandFactory final : public IClassFactory {
public:
    IFACEMETHODIMP QueryInterface(REFIID id, void** object) override {
        if (!object) return E_POINTER;
        if (id == IID_IUnknown || id == IID_IClassFactory) {
            *object = static_cast<IClassFactory*>(this);
            AddRef();
            return S_OK;
        }
        *object = nullptr;
        return E_NOINTERFACE;
    }
    // The one factory lives as long as the DLL, so it is not counted.
    IFACEMETHODIMP_(ULONG) AddRef() override { return 2; }
    IFACEMETHODIMP_(ULONG) Release() override { return 1; }

    IFACEMETHODIMP CreateInstance(IUnknown* outer, REFIID id, void** object) override {
        if (!object) return E_POINTER;
        *object = nullptr;
        if (outer) return CLASS_E_NOAGGREGATION;
        ConvertCommand* command = new (std::nothrow) ConvertCommand();
        if (!command) return E_OUTOFMEMORY;
        const HRESULT result = command->QueryInterface(id, object);
        command->Release();
        return result;
    }
    IFACEMETHODIMP LockServer(BOOL lock) override {
        if (lock) InterlockedIncrement(&g_locks); else InterlockedDecrement(&g_locks);
        return S_OK;
    }
};

CommandFactory g_factory;

}  // namespace

BOOL APIENTRY DllMain(HINSTANCE module, DWORD reason, LPVOID) {
    if (reason == DLL_PROCESS_ATTACH) {
        g_module = module;
        DisableThreadLibraryCalls(module);
    }
    return TRUE;
}

STDAPI DllGetClassObject(REFCLSID classId, REFIID id, void** object) {
    if (!object) return E_POINTER;
    *object = nullptr;
    if (classId != __uuidof(ConvertCommand)) return CLASS_E_CLASSNOTAVAILABLE;
    return g_factory.QueryInterface(id, object);
}

STDAPI DllCanUnloadNow() {
    return g_objects == 0 && g_locks == 0 ? S_OK : S_FALSE;
}
