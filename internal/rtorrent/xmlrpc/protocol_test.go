package xmlrpc

import "testing"

func TestEncodeDecodeRoundTripSimple(t *testing.T) {
	payload, err := EncodeMethodCall("demo", []any{"x", int64(1), true})
	if err != nil {
		t.Fatalf("EncodeMethodCall() error = %v", err)
	}
	if len(payload) == 0 {
		t.Fatalf("expected payload")
	}

	raw := []byte(`<?xml version="1.0"?><methodResponse><params><param><value><array><data><value><string>a</string></value><value><i4>2</i4></value></data></array></value></param></params></methodResponse>`)
	val, fault, err := DecodeMethodResponse(raw)
	if err != nil {
		t.Fatalf("DecodeMethodResponse() error = %v", err)
	}
	if fault != nil {
		t.Fatalf("unexpected fault: %v", fault)
	}
	arr, ok := val.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("unexpected decoded value: %#v", val)
	}
}

func TestDecodeFault(t *testing.T) {
	raw := []byte(`<?xml version="1.0"?><methodResponse><fault><value><struct><member><name>faultCode</name><value><i4>4</i4></value></member><member><name>faultString</name><value><string>oops</string></value></member></struct></value></fault></methodResponse>`)
	_, fault, err := DecodeMethodResponse(raw)
	if err != nil {
		t.Fatalf("DecodeMethodResponse() error = %v", err)
	}
	if fault == nil || fault.Code != 4 || fault.String != "oops" {
		t.Fatalf("unexpected fault: %#v", fault)
	}
}
