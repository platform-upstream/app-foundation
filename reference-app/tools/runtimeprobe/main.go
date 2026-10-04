package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: runtimeprobe BINARY TEMP_DIRECTORY")
		os.Exit(2)
	}
	binary, err := filepath.Abs(os.Args[1])
	if err != nil {
		fail(err)
	}
	times := make([]int64, 0, 5)
	for run := 1; run <= 5; run++ {
		elapsed, err := coldStart(binary, os.Args[2], run)
		if err != nil {
			fail(err)
		}
		times = append(times, elapsed.Milliseconds())
	}
	sorted := append([]int64(nil), times...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	fmt.Printf("cold_start_ms=%d,%d,%d,%d,%d\n", times[0], times[1], times[2], times[3], times[4])
	fmt.Printf("cold_start_min_ms=%d cold_start_median_ms=%d cold_start_max_ms=%d\n", sorted[0], sorted[2], sorted[4])
}

func coldStart(binary, directory string, run int) (time.Duration, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve loopback port: %w", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("release loopback port: %w", err)
	}
	logFile, err := os.Create(filepath.Join(directory, fmt.Sprintf("cold-%d.log", run)))
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	command := exec.Command(binary)
	command.Env = append(os.Environ(),
		"APP_ADDR="+address,
		fmt.Sprintf("APP_DB=%s", filepath.Join(directory, fmt.Sprintf("cold-%d.sqlite", run))),
	)
	command.Stdout = logFile
	command.Stderr = logFile
	started := time.Now()
	if err := command.Start(); err != nil {
		return 0, err
	}
	ready, err := waitForHealth("http://"+address+"/healthz", command)
	if signalErr := command.Process.Signal(syscall.SIGTERM); signalErr != nil && err == nil {
		err = signalErr
	}
	if waitErr := waitForExit(command); waitErr != nil && err == nil {
		err = waitErr
	}
	if err != nil {
		return 0, fmt.Errorf("cold start %d: %w", run, err)
	}
	return ready.Sub(started), nil
}

func waitForHealth(url string, command *exec.Cmd) (time.Time, error) {
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return time.Now(), nil
			}
		}
		if command.ProcessState != nil {
			return time.Time{}, fmt.Errorf("process exited before health")
		}
		time.Sleep(2 * time.Millisecond)
	}
	return time.Time{}, fmt.Errorf("health deadline exceeded")
}

func waitForExit(command *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		<-done
		return fmt.Errorf("graceful shutdown deadline exceeded")
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
