//go:build !linux

package main

import "errors"

func nativeCredentialVaultSet(service, account, value string) error {
	return errors.New("native remote credential vault is not implemented for this desktop platform")
}

func nativeCredentialVaultGet(service, account string) (string, error) {
	return "", errors.New("native remote credential vault is not implemented for this desktop platform")
}

func nativeCredentialVaultDelete(service, account string) error {
	return errors.New("native remote credential vault is not implemented for this desktop platform")
}
