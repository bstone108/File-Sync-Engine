package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"filesyncengine/internal/metabench"
)

func main() {
	var output string
	var timeout time.Duration
	flag.StringVar(&output, "output", "", "optional markdown report path")
	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "benchmark timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var results []metabench.Result
	for _, candidate := range metabench.DefaultCandidates() {
		result, err := metabench.RunCandidate(ctx, candidate, metabench.DefaultWorkloadConfig())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s benchmark failed: %v\n", candidate.Name(), err)
			os.Exit(1)
		}
		results = append(results, result)
	}

	wd, _ := os.Getwd()
	host := metabench.CollectHostFacts(wd)
	host.Kernel = readFirstLine("/proc/sys/kernel/osrelease")
	host.CPU = firstCPUModel()
	host.RAM = firstMemTotal()
	host.LoadAverage = readFirstLine("/proc/loadavg")
	report := metabench.FormatMarkdownReport(results, host)
	if output != "" {
		if err := os.WriteFile(output, []byte(report), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write report: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Print(report)
}

func readFirstLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	return line
}

func firstCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Hardware") || strings.HasPrefix(line, "Processor") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func firstMemTotal() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:"))
		}
	}
	return ""
}
