//go:build linux

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestDeleteSecretServiceItemsAttemptsEveryMatchAndAggregatesFailures(t *testing.T) {
	items := []dbus.ObjectPath{"/item/one", "/item/two", "/item/three"}
	var attempted []dbus.ObjectPath
	err := deleteSecretServiceItems(items, func(item dbus.ObjectPath) (dbus.ObjectPath, error) {
		attempted = append(attempted, item)
		switch item {
		case "/item/one":
			return noPromptPath, errors.New("delete failed")
		case "/item/two":
			return dbus.ObjectPath("/prompt/2"), nil
		default:
			return noPromptPath, nil
		}
	})
	if len(attempted) != len(items) || err == nil || !strings.Contains(err.Error(), "/item/one") || !strings.Contains(err.Error(), "/item/two") {
		t.Fatalf("attempted=%v err=%v", attempted, err)
	}
}
