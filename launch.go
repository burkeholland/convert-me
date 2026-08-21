package main

import "strings"

func ParseLaunchRequest(args []string) LaunchRequest {
	request := LaunchRequest{}
	paths := make([]string, 0)

	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		switch strings.ToLower(arg) {
		case "--convert":
			request.Mode = "convert"
		case "--custom":
			request.Mode = "custom"
		case "--settings":
			request.Mode = "settings"
		case "--tray":
			request.Mode = "tray"
		case "--register":
			request.Mode = "register"
		case "--unregister":
			request.Mode = "unregister"
		case "--format":
			if index+1 < len(args) {
				index++
				request.Format = string(NormalizeFormat(args[index]))
			}
		case "--":
			paths = append(paths, args[index+1:]...)
			index = len(args)
		default:
			if !strings.HasPrefix(arg, "--") && arg != "" {
				paths = append(paths, arg)
			}
		}
	}

	request.Files = uniquePaths(paths)
	if request.Mode == "" && len(request.Files) > 0 {
		request.Mode = "custom"
	}
	return request
}

func uniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, path)
	}
	return result
}
