package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func main() {
	myPID := os.Getpid()
	entries, _ := os.ReadDir("/proc")
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == myPID || pid == 1 {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		cmd := string(cmdline)
		if strings.Contains(cmd, "chromium") || strings.Contains(cmd, "leakless") {
			fmt.Printf("Killing PID %d: %.80s...\n", pid, cmd)
			syscall.Kill(pid, syscall.SIGKILL)
			killed++
		}
	}
	fmt.Printf("Killed %d processes\n", killed)
}
