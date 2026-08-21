//go:build !windows

package main

func coalesceExplorerLaunch(request LaunchRequest) (LaunchRequest, bool, error) {
	return request, true, nil
}
