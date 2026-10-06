// A small test program for ConvertMeCommand.dll. It is not part of the app.
//
//   probe describe --dll <path to the DLL>         ask the command for its title, icon and state
//   probe describe --registered                    the same, through the registered package
//   probe invoke --dll <path> <list of paths>      call the command with the files named in a
//   probe invoke --registered <list of paths>      UTF-8 text file, one full path on each line
//
// When it is started with --files-from, it stands in for ConvertMe.exe: it saves the list
// file and its own command line in the folder named by CONVERTME_PROBE_OUT. The build
// uses that to check what the command hands over, without opening the real app.

#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <shlobj.h>

#include <fcntl.h>
#include <io.h>
#include <cstdio>
#include <string>
#include <vector>

#include "clsid.h"

namespace {

class __declspec(uuid(CONVERTME_COMMAND_CLSID)) ConvertCommand;

std::string Utf8(const std::wstring& text) {
    if (text.empty()) return {};
    const int size = WideCharToMultiByte(CP_UTF8, 0, text.data(), static_cast<int>(text.size()), nullptr, 0, nullptr, nullptr);
    std::string out(static_cast<size_t>(size), '\0');
    WideCharToMultiByte(CP_UTF8, 0, text.data(), static_cast<int>(text.size()), out.data(), size, nullptr, nullptr);
    return out;
}

std::wstring Wide(const std::string& text) {
    if (text.empty()) return {};
    const int size = MultiByteToWideChar(CP_UTF8, 0, text.data(), static_cast<int>(text.size()), nullptr, 0);
    std::wstring out(static_cast<size_t>(size), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, text.data(), static_cast<int>(text.size()), out.data(), size);
    return out;
}

bool ReadAll(const std::wstring& path, std::string* content) {
    const HANDLE file = CreateFileW(path.c_str(), GENERIC_READ, FILE_SHARE_READ, nullptr, OPEN_EXISTING, 0, nullptr);
    if (file == INVALID_HANDLE_VALUE) return false;
    char buffer[65536];
    DWORD count = 0;
    while (ReadFile(file, buffer, sizeof(buffer), &count, nullptr) && count > 0) content->append(buffer, count);
    CloseHandle(file);
    return true;
}

bool WriteAll(const std::wstring& path, const std::string& content) {
    const HANDLE file = CreateFileW(path.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) return false;
    DWORD count = 0;
    const bool ok = WriteFile(file, content.data(), static_cast<DWORD>(content.size()), &count, nullptr) && count == content.size();
    CloseHandle(file);
    return ok;
}

// Standing in for ConvertMe.exe: keep what the command handed over.
int StandIn(const std::wstring& list) {
    wchar_t folder[MAX_PATH];
    const DWORD length = GetEnvironmentVariableW(L"CONVERTME_PROBE_OUT", folder, ARRAYSIZE(folder));
    if (length == 0 || length >= ARRAYSIZE(folder)) return 2;
    std::string content;
    if (!ReadAll(list, &content)) return 3;
    const std::wstring out(folder);
    if (!WriteAll(out + L"\\convertme-files-captured.txt", content)) return 4;
    if (!WriteAll(out + L"\\command-line.txt", Utf8(GetCommandLineW()))) return 4;
    DeleteFileW(list.c_str());
    return 0;
}

HRESULT CreateCommand(const std::wstring& dll, IExplorerCommand** command) {
    if (dll.empty()) {
        return CoCreateInstance(__uuidof(ConvertCommand), nullptr, CLSCTX_LOCAL_SERVER, IID_PPV_ARGS(command));
    }
    const HMODULE module = LoadLibraryW(dll.c_str());
    if (!module) return HRESULT_FROM_WIN32(GetLastError());
    using GetClassObject = HRESULT(STDAPICALLTYPE*)(REFCLSID, REFIID, void**);
    const auto getClassObject = reinterpret_cast<GetClassObject>(GetProcAddress(module, "DllGetClassObject"));
    if (!getClassObject) return HRESULT_FROM_WIN32(GetLastError());
    IClassFactory* factory = nullptr;
    HRESULT result = getClassObject(__uuidof(ConvertCommand), IID_PPV_ARGS(&factory));
    if (FAILED(result)) return result;
    result = factory->CreateInstance(nullptr, IID_PPV_ARGS(command));
    factory->Release();
    return result;
}

HRESULT ItemsFromList(const std::wstring& listPath, IShellItemArray** items, size_t* count) {
    std::string content;
    if (!ReadAll(listPath, &content)) return HRESULT_FROM_WIN32(ERROR_FILE_NOT_FOUND);
    if (content.compare(0, 3, "\xEF\xBB\xBF") == 0) content.erase(0, 3);
    std::vector<PIDLIST_ABSOLUTE> ids;
    size_t start = 0;
    HRESULT result = S_OK;
    while (start < content.size() && SUCCEEDED(result)) {
        size_t end = content.find('\n', start);
        if (end == std::string::npos) end = content.size();
        std::string line = content.substr(start, end - start);
        if (!line.empty() && line.back() == '\r') line.pop_back();
        start = end + 1;
        if (line.empty()) continue;
        PIDLIST_ABSOLUTE id = nullptr;
        result = SHParseDisplayName(Wide(line).c_str(), nullptr, &id, 0, nullptr);
        if (SUCCEEDED(result)) ids.push_back(id);
    }
    if (SUCCEEDED(result)) {
        result = SHCreateShellItemArrayFromIDLists(static_cast<UINT>(ids.size()),
            const_cast<PCIDLIST_ABSOLUTE*>(ids.data()), items);
    }
    for (PIDLIST_ABSOLUTE id : ids) CoTaskMemFree(id);
    *count = ids.size();
    return result;
}

void Print(const char* name, HRESULT result, PWSTR text) {
    printf("%s=0x%08lX %s\n", name, static_cast<unsigned long>(result), text ? Utf8(text).c_str() : "");
    CoTaskMemFree(text);
}

int Usage() {
    fputs("usage: probe describe|invoke (--dll <path> | --registered) [<list of paths>]\n", stderr);
    return 64;
}

}  // namespace

int wmain(int argc, wchar_t** argv) {
    for (int i = 1; i + 1 < argc; i++) {
        if (wcscmp(argv[i], L"--files-from") == 0) return StandIn(argv[i + 1]);
    }
    if (argc < 3) return Usage();
    const std::wstring action = argv[1];
    std::wstring dll;
    int next = 3;
    if (wcscmp(argv[2], L"--dll") == 0 && argc >= 4) {
        dll = argv[3];
        next = 4;
    } else if (wcscmp(argv[2], L"--registered") != 0) {
        return Usage();
    }

    _setmode(_fileno(stdout), _O_BINARY);
    if (FAILED(CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE))) return 1;
    IExplorerCommand* command = nullptr;
    HRESULT result = CreateCommand(dll, &command);
    if (FAILED(result)) {
        printf("create=0x%08lX\n", static_cast<unsigned long>(result));
        return 1;
    }

    int exitCode = 0;
    if (action == L"describe") {
        PWSTR text = nullptr;
        result = command->GetTitle(nullptr, &text);
        Print("title", result, text);
        text = nullptr;
        result = command->GetIcon(nullptr, &text);
        Print("icon", result, text);
        EXPCMDSTATE state = ECS_HIDDEN;
        result = command->GetState(nullptr, FALSE, &state);
        printf("state=0x%08lX %lu\n", static_cast<unsigned long>(result), static_cast<unsigned long>(state));
        EXPCMDFLAGS flags = 0;
        result = command->GetFlags(&flags);
        printf("flags=0x%08lX %lu\n", static_cast<unsigned long>(result), static_cast<unsigned long>(flags));
    } else if (action == L"invoke" && next < argc) {
        IShellItemArray* items = nullptr;
        size_t count = 0;
        result = ItemsFromList(argv[next], &items, &count);
        printf("items=0x%08lX %zu\n", static_cast<unsigned long>(result), count);
        if (SUCCEEDED(result)) {
            result = command->Invoke(items, nullptr);
            printf("invoke=0x%08lX\n", static_cast<unsigned long>(result));
            items->Release();
        }
        if (FAILED(result)) exitCode = 1;
    } else {
        exitCode = Usage();
    }
    command->Release();
    CoUninitialize();
    return exitCode;
}
