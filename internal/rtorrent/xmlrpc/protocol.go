package xmlrpc

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Fault is an XML-RPC method fault.
type Fault struct {
	Code   int
	String string
}

func (f *Fault) Error() string {
	return fmt.Sprintf("xml-rpc fault %d: %s", f.Code, f.String)
}

func EncodeMethodCall(method string, args []any) ([]byte, error) {
	if method == "" {
		return nil, errors.New("empty method")
	}

	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0"?>`)
	b.WriteString("<methodCall>")
	b.WriteString("<methodName>")
	escapeText(&b, method)
	b.WriteString("</methodName>")
	b.WriteString("<params>")
	for _, arg := range args {
		b.WriteString("<param>")
		if err := encodeValue(&b, arg); err != nil {
			return nil, err
		}
		b.WriteString("</param>")
	}
	b.WriteString("</params>")
	b.WriteString("</methodCall>")
	return b.Bytes(), nil
}

func encodeValue(w *bytes.Buffer, v any) error {
	w.WriteString("<value>")
	switch t := v.(type) {
	case nil:
		w.WriteString("<nil/>")
	case string:
		w.WriteString("<string>")
		escapeText(w, t)
		w.WriteString("</string>")
	case bool:
		w.WriteString("<boolean>")
		if t {
			w.WriteByte('1')
		} else {
			w.WriteByte('0')
		}
		w.WriteString("</boolean>")
	case int:
		w.WriteString("<i4>")
		w.WriteString(strconv.Itoa(t))
		w.WriteString("</i4>")
	case int32:
		w.WriteString("<i4>")
		w.WriteString(strconv.FormatInt(int64(t), 10))
		w.WriteString("</i4>")
	case int64:
		w.WriteString("<i8>")
		w.WriteString(strconv.FormatInt(t, 10))
		w.WriteString("</i8>")
	case float64:
		w.WriteString("<double>")
		w.WriteString(strconv.FormatFloat(t, 'f', -1, 64))
		w.WriteString("</double>")
	case []byte:
		w.WriteString("<base64>")
		w.WriteString(base64.StdEncoding.EncodeToString(t))
		w.WriteString("</base64>")
	case []any:
		w.WriteString("<array><data>")
		for _, item := range t {
			if err := encodeValue(w, item); err != nil {
				return err
			}
		}
		w.WriteString("</data></array>")
	case map[string]any:
		w.WriteString("<struct>")
		for k, vv := range t {
			w.WriteString("<member><name>")
			escapeText(w, k)
			w.WriteString("</name>")
			if err := encodeValue(w, vv); err != nil {
				return err
			}
			w.WriteString("</member>")
		}
		w.WriteString("</struct>")
	default:
		return fmt.Errorf("unsupported xml-rpc arg type %T", v)
	}
	w.WriteString("</value>")
	return nil
}

func DecodeMethodResponse(data []byte) (result any, fault *Fault, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, tokenErr := dec.Token()
		if tokenErr == io.EOF {
			return nil, nil, errors.New("invalid xml-rpc response")
		}
		if tokenErr != nil {
			return nil, nil, tokenErr
		}

		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local != "methodResponse" {
			continue
		}
		return parseMethodResponse(dec)
	}
}

func parseMethodResponse(dec *xml.Decoder) (any, *Fault, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "params":
				return parseParams(dec)
			case "fault":
				return parseFault(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "methodResponse" {
				return nil, nil, errors.New("missing params/fault in methodResponse")
			}
		}
	}
}

func parseParams(dec *xml.Decoder) (any, *Fault, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "param" {
				val, err := parseParam(dec)
				if err != nil {
					return nil, nil, err
				}
				return val, nil, nil
			}
		case xml.EndElement:
			if t.Name.Local == "params" {
				return nil, nil, errors.New("empty params")
			}
		}
	}
}

func parseParam(dec *xml.Decoder) (any, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "value" {
				return parseValue(dec)
			}
		case xml.EndElement:
			if t.Name.Local == "param" {
				return nil, errors.New("missing value in param")
			}
		}
	}
}

func parseFault(dec *xml.Decoder) (any, *Fault, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "value" {
				continue
			}
			fv, err := parseValue(dec)
			if err != nil {
				return nil, nil, err
			}
			obj, ok := fv.(map[string]any)
			if !ok {
				return nil, nil, errors.New("invalid fault value")
			}
			fault := &Fault{}
			fault.Code = toInt(obj["faultCode"])
			fault.String = toString(obj["faultString"])
			return nil, fault, nil
		case xml.EndElement:
			if t.Name.Local == "fault" {
				return nil, nil, errors.New("empty fault")
			}
		}
	}
}

func parseValue(dec *xml.Decoder) (any, error) {
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			text.Write([]byte(t))
		case xml.StartElement:
			return parseTypedValue(dec, t)
		case xml.EndElement:
			if t.Name.Local == "value" {
				return strings.TrimSpace(text.String()), nil
			}
		}
	}
}

func parseTypedValue(dec *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "string", "dateTime.iso8601":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		return s, consumeValueEnd(dec)
	case "int", "i4", "i8":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, err
		}
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return n, nil
	case "boolean":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		v := strings.TrimSpace(s)
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return v == "1" || strings.EqualFold(v, "true"), nil
	case "double":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, err
		}
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return f, nil
	case "base64":
		var s string
		if err := dec.DecodeElement(&s, &start); err != nil {
			return nil, err
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return nil, err
		}
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return b, nil
	case "nil":
		if err := dec.Skip(); err != nil {
			return nil, err
		}
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return nil, nil
	case "array":
		arr, err := parseArray(dec, start)
		if err != nil {
			return nil, err
		}
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return arr, nil
	case "struct":
		obj, err := parseStruct(dec, start)
		if err != nil {
			return nil, err
		}
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return obj, nil
	default:
		if err := dec.Skip(); err != nil {
			return nil, err
		}
		if err := consumeValueEnd(dec); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func parseArray(dec *xml.Decoder, start xml.StartElement) ([]any, error) {
	items := make([]any, 0)
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "value" {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				items = append(items, v)
			}
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return items, nil
			}
		}
	}
}

func parseStruct(dec *xml.Decoder, start xml.StartElement) (map[string]any, error) {
	obj := make(map[string]any)
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "member" {
				continue
			}
			k, v, err := parseMember(dec)
			if err != nil {
				return nil, err
			}
			obj[k] = v
		case xml.EndElement:
			if t.Name.Local == start.Name.Local {
				return obj, nil
			}
		}
	}
}

func parseMember(dec *xml.Decoder) (string, any, error) {
	var name string
	var value any
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "name":
				if err := dec.DecodeElement(&name, &t); err != nil {
					return "", nil, err
				}
			case "value":
				value, err = parseValue(dec)
				if err != nil {
					return "", nil, err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "member" {
				return name, value, nil
			}
		}
	}
}

func consumeValueEnd(dec *xml.Decoder) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if end, ok := tok.(xml.EndElement); ok && end.Name.Local == "value" {
			return nil
		}
	}
}

func escapeText(b *bytes.Buffer, s string) {
	_ = xml.EscapeText(b, []byte(s))
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(t))
		return i
	default:
		return 0
	}
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", v)
	}
}
