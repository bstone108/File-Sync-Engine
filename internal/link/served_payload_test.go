package link

import (
	"testing"
	"time"
)

func TestServedPayloadSkipsUsedAndUnknownNamespaces(t *testing.T) {
	code := "ABCD2345EFGH6789"
	namespace := Namespace(code)
	service := NewService(nil)
	service.codes[NormalizeCode(code)] = &issuedCode{
		code:    NormalizeCode(code),
		payload: []byte("sealed"),
		expires: time.Now().Add(time.Hour),
	}
	got, ok := service.ServedPayload(namespace)
	if !ok || string(got) != "sealed" {
		t.Fatalf("payload ok=%v %q", ok, got)
	}
	service.codes[NormalizeCode(code)].used = true
	if _, ok := service.ServedPayload(namespace); ok {
		t.Fatal("used code was served")
	}
	if _, ok := service.ServedPayload("other/namespace"); ok {
		t.Fatal("foreign namespace was served")
	}
	if _, ok := service.ServedPayload(CodeNamespacePrefix + "../secret"); ok {
		t.Fatal("path namespace was served")
	}
}
