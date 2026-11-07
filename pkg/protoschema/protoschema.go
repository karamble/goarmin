// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package protoschema reads proto2 .proto files at run time and renders protobuf messages
// field by field against them, without generated code. It understands packages, nested
// messages and enums, oneofs and field options, and skips services, extends, reserved and
// option statements; that is enough for the Garmin .proto files Gadgetbridge ships. goarmin
// uses it for debugging and protocol research (tools/snoopdecode), not on the data path.
package protoschema

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type protoField struct {
	name, typ, label string
	num              int
	scope            string // message full name, for relative type resolution
}

type protoMessage struct {
	name   string
	fields map[int]*protoField
}

// Schema holds the messages and enums of a set of .proto files.
type Schema struct {
	messages map[string]*protoMessage
	enums    map[string]map[int]string
}

// Load parses every .proto file in dir.
func Load(dir string) (*Schema, error) {
	s := &Schema{messages: map[string]*protoMessage{}, enums: map[string]map[int]string{}}
	files, err := filepath.Glob(filepath.Join(dir, "*.proto"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		p := &protoParser{toks: tokenize(string(b)), s: s}
		if err := p.file(); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
	}
	return s, nil
}

func tokenize(src string) []string {
	var toks []string
	r := []rune(src)
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '/' && i+1 < len(r) && r[i+1] == '/':
			for i < len(r) && r[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			i += 2
			for i+1 < len(r) && !(r[i] == '*' && r[i+1] == '/') {
				i++
			}
			i += 2
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(r) && r[j] != c {
				if r[j] == '\\' {
					j++
				}
				j++
			}
			toks = append(toks, string(r[i:j+1]))
			i = j + 1
		case unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '.' || c == '-':
			j := i
			for j < len(r) && (unicode.IsLetter(r[j]) || unicode.IsDigit(r[j]) || r[j] == '_' || r[j] == '.' || r[j] == '-') {
				j++
			}
			toks = append(toks, string(r[i:j]))
			i = j
		default:
			toks = append(toks, string(c))
			i++
		}
	}
	return toks
}

type protoParser struct {
	toks []string
	pos  int
	pkg  string
	s    *Schema
}

func (p *protoParser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *protoParser) next() string {
	t := p.peek()
	p.pos++
	return t
}

func (p *protoParser) skipStatement() { // to ';' or over a balanced {...}
	depth := 0
	for p.pos < len(p.toks) {
		t := p.next()
		switch t {
		case "{":
			depth++
		case "}":
			depth--
			if depth <= 0 {
				return
			}
		case ";":
			if depth == 0 {
				return
			}
		}
	}
}

func (p *protoParser) file() error {
	for p.pos < len(p.toks) {
		switch t := p.next(); t {
		case "package":
			p.pkg = p.next()
			p.next()
		case "message":
			if err := p.message(p.pkg); err != nil {
				return err
			}
		case "enum":
			p.enum(p.pkg)
		case ";":
		default:
			p.skipStatement()
		}
	}
	return nil
}

func join(scope, name string) string {
	if scope == "" {
		return name
	}
	return scope + "." + name
}

func (p *protoParser) message(scope string) error {
	full := join(scope, p.next())
	if p.next() != "{" {
		return fmt.Errorf("expected { after message %s", full)
	}
	m := &protoMessage{name: full, fields: map[int]*protoField{}}
	p.s.messages[full] = m
	return p.body(full, m)
}

func (p *protoParser) body(full string, m *protoMessage) error {
	for p.pos < len(p.toks) {
		t := p.peek()
		switch t {
		case "}":
			p.next()
			return nil
		case "message":
			p.next()
			if err := p.message(full); err != nil {
				return err
			}
		case "enum":
			p.next()
			p.enum(full)
		case "oneof":
			p.next()
			p.next() // name
			p.next() // {
			if err := p.body(full, m); err != nil {
				return err
			}
		case "option", "reserved", "extensions", "extend":
			p.skipStatement()
		case ";":
			p.next()
		default:
			f := &protoField{scope: full}
			if t == "optional" || t == "repeated" || t == "required" {
				f.label = p.next()
			}
			if p.peek() == "map" {
				p.skipStatement()
				continue
			}
			f.typ = p.next()
			f.name = p.next()
			if p.next() != "=" {
				return fmt.Errorf("%s: expected = after field %s", full, f.name)
			}
			n, err := strconv.Atoi(p.next())
			if err != nil {
				return fmt.Errorf("%s: bad field number for %s", full, f.name)
			}
			f.num = n
			for p.peek() != ";" && p.pos < len(p.toks) {
				p.next() // field options
			}
			p.next()
			m.fields[n] = f
		}
	}
	return nil
}

func (p *protoParser) enum(scope string) {
	full := join(scope, p.next())
	p.next() // {
	vals := map[int]string{}
	for p.pos < len(p.toks) {
		t := p.next()
		if t == "}" {
			break
		}
		if t == "option" || t == "reserved" {
			p.skipStatement()
			continue
		}
		if p.peek() == "=" {
			p.next()
			if n, err := strconv.Atoi(p.next()); err == nil {
				if _, dup := vals[n]; !dup {
					vals[n] = t
				}
			}
			for p.peek() != ";" && p.pos < len(p.toks) {
				p.next()
			}
			p.next()
		}
	}
	p.s.enums[full] = vals
}

// Resolve finds the full name of the message or enum a field type refers to, searching from
// scope outwards as protoc does. isEnum reports which of the two it is; full is empty if the
// type is unknown.
func (s *Schema) Resolve(scope, typ string) (string, bool) {
	t := strings.TrimPrefix(typ, ".")
	for sc := scope; ; {
		cand := join(sc, t)
		if _, ok := s.messages[cand]; ok {
			return cand, false
		}
		if _, ok := s.enums[cand]; ok {
			return cand, true
		}
		if sc == "" {
			break
		}
		if i := strings.LastIndex(sc, "."); i >= 0 {
			sc = sc[:i]
		} else {
			sc = ""
		}
	}
	return "", false
}

const garminEpoch = 631065600

// Render pretty-prints a protobuf message of type msgName (full name, e.g.
// "garmin_vivomovehr.Smart") as indented "name: value" lines. Unknown fields are shown by
// number; enums by name; GDI UUIDs as a most/least significant pair.
func (s *Schema) Render(msgName string, b []byte) []string {
	var out []string
	s.render(msgName, b, "", &out)
	return out
}

// HasMessage reports whether the schema defines a message with this full name.
func (s *Schema) HasMessage(name string) bool { return s.messages[name] != nil }

// FieldName returns the name of field num in message msgName.
func (s *Schema) FieldName(msgName string, num int) (string, bool) {
	m := s.messages[msgName]
	if m == nil || m.fields[num] == nil {
		return "", false
	}
	return m.fields[num].name, true
}

// GadgetbridgeDir returns the Garmin .proto directory of a Gadgetbridge clone at the
// conventional location (~/go/src/codeberg.org/Freeyourgadget/Gadgetbridge), or "" if absent.
func GadgetbridgeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	d := filepath.Join(home, "go/src/codeberg.org/Freeyourgadget/Gadgetbridge/GBProtobufGenerator/src/main/proto/garmin")
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return ""
}

func (s *Schema) render(msgName string, b []byte, indent string, out *[]string) {
	m := s.messages[msgName]
	for i := 0; i < len(b); {
		tag, n := uvarint(b[i:])
		if n <= 0 {
			*out = append(*out, indent+"!! bad tag")
			return
		}
		i += n
		fn, wt := int(tag>>3), tag&7
		var fd *protoField
		if m != nil {
			fd = m.fields[fn]
		}
		name := fmt.Sprintf("f%d", fn)
		if fd != nil {
			name = fd.name
		}
		switch wt {
		case 0:
			v, n := uvarint(b[i:])
			if n <= 0 {
				*out = append(*out, indent+"!! bad varint")
				return
			}
			i += n
			*out = append(*out, indent+name+": "+s.scalarVarint(fd, v))
		case 1:
			if i+8 > len(b) {
				return
			}
			v := binary.LittleEndian.Uint64(b[i:])
			i += 8
			val := fmt.Sprintf("0x%016x", v)
			if fd != nil && fd.typ == "double" {
				val = fmt.Sprint(math.Float64frombits(v))
			}
			*out = append(*out, indent+name+": "+val)
		case 5:
			if i+4 > len(b) {
				return
			}
			v := binary.LittleEndian.Uint32(b[i:])
			i += 4
			val := fmt.Sprintf("0x%08x", v)
			if fd != nil && fd.typ == "float" {
				val = fmt.Sprint(math.Float32frombits(v))
			} else if fd != nil && (fd.typ == "sfixed32") {
				val = fmt.Sprint(int32(v))
			}
			*out = append(*out, indent+name+": "+val)
		case 2:
			l, n := uvarint(b[i:])
			if n <= 0 || i+n+int(l) > len(b) {
				*out = append(*out, indent+"!! bad length")
				return
			}
			i += n
			sub := b[i : i+int(l)]
			i += int(l)
			if fd != nil {
				if full, isEnum := s.Resolve(fd.scope, fd.typ); full != "" && !isEnum {
					if full == "GDI.Proto.DataTypes.UUID" {
						*out = append(*out, indent+name+": "+uuidPair(sub))
						continue
					}
					*out = append(*out, indent+name+" {")
					s.render(full, sub, indent+"  ", out)
					*out = append(*out, indent+"}")
					continue
				}
				switch fd.typ {
				case "string":
					*out = append(*out, indent+name+": "+strconv.Quote(string(sub)))
					continue
				case "bytes":
					*out = append(*out, indent+name+": "+bytesShort(sub))
					continue
				}
			}
			*out = append(*out, indent+name+": "+bytesShort(sub))
		default:
			*out = append(*out, indent+fmt.Sprintf("!! wire type %d", wt))
			return
		}
	}
}

func (s *Schema) scalarVarint(fd *protoField, v uint64) string {
	if fd == nil {
		return fmt.Sprint(v)
	}
	if full, isEnum := s.Resolve(fd.scope, fd.typ); isEnum {
		if n, ok := s.enums[full][int(v)]; ok {
			return fmt.Sprintf("%s(%d)", n, v)
		}
		return fmt.Sprintf("%d (enum %s)", v, full)
	}
	switch fd.typ {
	case "bool":
		return fmt.Sprint(v != 0)
	case "sint32", "sint64":
		return fmt.Sprint(int64(v>>1) ^ -int64(v&1))
	case "int32":
		return fmt.Sprint(int32(v))
	case "int64":
		return fmt.Sprint(int64(v))
	}
	val := fmt.Sprint(v)
	if strings.Contains(fd.name, "time") && v > 900000000 && v < 2000000000 {
		val += " (" + time.Unix(int64(v)+garminEpoch, 0).UTC().Format("2006-01-02 15:04:05Z") + " if Garmin epoch)"
	}
	return val
}

func uuidPair(b []byte) string {
	var ms, ls uint64
	for _, f := range protoFieldsRaw(b) {
		switch f.num {
		case 1:
			ms = f.u64
		case 2:
			ls = f.u64
		}
	}
	return fmt.Sprintf("uuid{0x%x, 0x%x}", ms, ls)
}

type rawField struct {
	num int
	u64 uint64
}

func protoFieldsRaw(b []byte) (out []rawField) {
	for i := 0; i < len(b); {
		tag, n := uvarint(b[i:])
		if n <= 0 {
			return
		}
		i += n
		fn, wt := int(tag>>3), tag&7
		switch wt {
		case 0:
			v, n := uvarint(b[i:])
			if n <= 0 {
				return
			}
			i += n
			out = append(out, rawField{fn, v})
		case 1:
			if i+8 > len(b) {
				return
			}
			out = append(out, rawField{fn, binary.LittleEndian.Uint64(b[i:])})
			i += 8
		case 5:
			if i+4 > len(b) {
				return
			}
			out = append(out, rawField{fn, uint64(binary.LittleEndian.Uint32(b[i:]))})
			i += 4
		case 2:
			l, n := uvarint(b[i:])
			if n <= 0 {
				return
			}
			i += n + int(l)
		default:
			return
		}
	}
	return
}

func bytesShort(b []byte) string {
	if len(b) <= 48 {
		return fmt.Sprintf("bytes(%d) % x", len(b), b)
	}
	return fmt.Sprintf("bytes(%d) % x …", len(b), b[:48])
}

func uvarint(b []byte) (uint64, int) { return binary.Uvarint(b) }
