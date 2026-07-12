//go:build linux

package main

import (
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceName = "org.freedesktop.secrets"
	secretServicePath = dbus.ObjectPath("/org/freedesktop/secrets")
	secretCollection  = dbus.ObjectPath("/org/freedesktop/secrets/aliases/default")
	noPromptPath      = dbus.ObjectPath("/")
)

type secretServiceValue struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func nativeCredentialVaultSet(service, account, value string) error {
	conn, session, err := openSecretServiceSession()
	if err != nil {
		return err
	}
	defer conn.Close()
	properties := map[string]dbus.Variant{
		"org.freedesktop.Secret.Item.Label":      dbus.MakeVariant(service),
		"org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(map[string]string{"service": service, "account": account}),
	}
	secret := secretServiceValue{Session: session, Value: []byte(value), ContentType: "text/plain; charset=utf8"}
	var item, prompt dbus.ObjectPath
	if err := conn.Object(secretServiceName, secretCollection).Call("org.freedesktop.Secret.Collection.CreateItem", 0, properties, secret, true).Store(&item, &prompt); err != nil {
		return fmt.Errorf("Secret Service CreateItem: %w", err)
	}
	if prompt != noPromptPath {
		return errors.New("Secret Service requires an interactive prompt; unlock the login keyring and retry")
	}
	return nil
}

func nativeCredentialVaultGet(service, account string) (string, error) {
	conn, session, err := openSecretServiceSession()
	if err != nil {
		return "", err
	}
	defer conn.Close()
	unlocked, locked, err := searchSecretItems(conn, service, account)
	if err != nil {
		return "", err
	}
	if len(unlocked) == 0 {
		if len(locked) > 0 {
			return "", errors.New("Secret Service item is locked; unlock the login keyring and retry")
		}
		return "", errCredentialNotFound
	}
	var secret secretServiceValue
	if err := conn.Object(secretServiceName, unlocked[0]).Call("org.freedesktop.Secret.Item.GetSecret", 0, session).Store(&secret); err != nil {
		return "", fmt.Errorf("Secret Service GetSecret: %w", err)
	}
	return string(secret.Value), nil
}

func nativeCredentialVaultDelete(service, account string) error {
	conn, _, err := openSecretServiceSession()
	if err != nil {
		return err
	}
	defer conn.Close()
	unlocked, locked, err := searchSecretItems(conn, service, account)
	if err != nil {
		return err
	}
	items := append(unlocked, locked...)
	if len(items) == 0 {
		return errCredentialNotFound
	}
	deleteErr := deleteSecretServiceItems(items, func(item dbus.ObjectPath) (dbus.ObjectPath, error) {
		var prompt dbus.ObjectPath
		if err := conn.Object(secretServiceName, item).Call("org.freedesktop.Secret.Item.Delete", 0).Store(&prompt); err != nil {
			return noPromptPath, err
		}
		return prompt, nil
	})
	remainingUnlocked, remainingLocked, searchErr := searchSecretItems(conn, service, account)
	if searchErr != nil {
		return errors.Join(deleteErr, fmt.Errorf("verify Secret Service deletion: %w", searchErr))
	}
	if remaining := len(remainingUnlocked) + len(remainingLocked); remaining > 0 {
		return errors.Join(deleteErr, fmt.Errorf("Secret Service deletion incomplete: %d matching item(s) remain", remaining))
	}
	return nil
}

func deleteSecretServiceItems(items []dbus.ObjectPath, deleteItem func(dbus.ObjectPath) (dbus.ObjectPath, error)) error {
	var failures []error
	for _, item := range items {
		prompt, err := deleteItem(item)
		if err != nil {
			failures = append(failures, fmt.Errorf("Secret Service Delete %s: %w", item, err))
			continue
		}
		if prompt != noPromptPath {
			failures = append(failures, fmt.Errorf("Secret Service Delete %s requires an interactive prompt; unlock the login keyring and retry", item))
		}
	}
	return errors.Join(failures...)
}

func openSecretServiceSession() (*dbus.Conn, dbus.ObjectPath, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, "", fmt.Errorf("connect to Freedesktop Secret Service session bus: %w", err)
	}
	var output dbus.Variant
	var session dbus.ObjectPath
	if err := conn.Object(secretServiceName, secretServicePath).Call("org.freedesktop.Secret.Service.OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&output, &session); err != nil {
		conn.Close()
		return nil, "", fmt.Errorf("open Freedesktop Secret Service session: %w", err)
	}
	return conn, session, nil
}

func searchSecretItems(conn *dbus.Conn, service, account string) ([]dbus.ObjectPath, []dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	err := conn.Object(secretServiceName, secretServicePath).Call("org.freedesktop.Secret.Service.SearchItems", 0, map[string]string{"service": service, "account": account}).Store(&unlocked, &locked)
	if err != nil {
		return nil, nil, fmt.Errorf("search Freedesktop Secret Service: %w", err)
	}
	return unlocked, locked, nil
}
