package main

import (
	"fmt"
	"strconv"
	"strings"
)

type desktopVersion struct {
	Year  int
	Month int
	Day   int
	N     int
}

func parseDesktopVersion(raw string) (desktopVersion, bool) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "v")
	trimmed = strings.TrimPrefix(trimmed, "V")
	parts := strings.Split(trimmed, ".")
	if len(parts) != 4 {
		return desktopVersion{}, false
	}
	nums := make([]int, 4)
	for i, part := range parts {
		if part == "" {
			return desktopVersion{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return desktopVersion{}, false
		}
		nums[i] = n
	}
	return desktopVersion{Year: nums[0], Month: nums[1], Day: nums[2], N: nums[3]}, true
}

func (v desktopVersion) String() string {
	return fmt.Sprintf("%d.%d.%d.%d", v.Year, v.Month, v.Day, v.N)
}

func (v desktopVersion) Padded() string {
	return fmt.Sprintf("%d.%02d.%02d.%02d", v.Year, v.Month, v.Day, v.N)
}

func (v desktopVersion) FilenameCandidates() []string {
	unpadded := v.String()
	padded := v.Padded()
	if unpadded == padded {
		return []string{unpadded}
	}
	return []string{unpadded, padded}
}

func compareDesktopVersions(current, candidate string) int {
	left, leftOK := parseDesktopVersion(current)
	right, rightOK := parseDesktopVersion(candidate)
	if !leftOK && !rightOK {
		return strings.Compare(strings.TrimPrefix(strings.ToLower(current), "v"), strings.TrimPrefix(strings.ToLower(candidate), "v"))
	}
	if !leftOK {
		return -1
	}
	if !rightOK {
		return 1
	}
	if left.Year != right.Year {
		return left.Year - right.Year
	}
	if left.Month != right.Month {
		return left.Month - right.Month
	}
	if left.Day != right.Day {
		return left.Day - right.Day
	}
	return left.N - right.N
}
