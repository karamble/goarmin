// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cobs

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Vector from Gadgetbridge's CobsCoDecTest: a DEVICE_INFORMATION frame (ending in 00 00 04 B8)
// and its encoding.
const (
	gbDecoded = "2C00A0139600310F684C1BCA840508020B496E7374696E637420325308496E7374696E6374023253000004B8"
	gbEncoded = "00022C04A0139623310F684C1BCA840508020B496E7374696E637420325308496E7374696E6374023253010304B800"
)

func TestEncodeGadgetbridgeVector(t *testing.T) {
	got := Encode(mustHex(t, gbDecoded))
	if want := mustHex(t, gbEncoded); !bytes.Equal(got, want) {
		t.Fatalf("Encode\n got  % X\n want % X", got, want)
	}
}

func TestDecodeGadgetbridgeVector(t *testing.T) {
	enc := mustHex(t, gbEncoded)
	got, err := Decode(enc[1 : len(enc)-1])
	if err != nil {
		t.Fatal(err)
	}
	if want := mustHex(t, gbDecoded); !bytes.Equal(got, want) {
		t.Fatalf("Decode\n got  % X\n want % X", got, want)
	}
}

func TestDecodeEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		seg  []byte
		want []byte
	}{
		{"zero in the middle", []byte{0x03, 0x11, 0x22, 0x02, 0x33}, []byte{0x11, 0x22, 0x00, 0x33}},
		{"single zero", []byte{0x01, 0x01}, []byte{0x00}},
		{"254-byte run then end code", append(append([]byte{0xFF}, bytes.Repeat([]byte{0x42}, 254)...), 0x01),
			bytes.Repeat([]byte{0x42}, 254)},
		{"empty", nil, []byte{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Decode(c.seg)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("got % X, want % X", got, c.want)
			}
		})
	}
	if _, err := Decode([]byte{0x05, 0x11}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("truncated: err = %v", err)
	}
	if _, err := Decode([]byte{0x03, 0x11, 0x00}); !errors.Is(err, ErrZeroInSegment) {
		t.Fatalf("zero inside: err = %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 600; n++ {
		src := make([]byte, n)
		for i := range src {
			if rng.IntN(5) == 0 {
				src[i] = 0
			} else {
				src[i] = byte(rng.IntN(255) + 1)
			}
		}
		enc := Encode(src)
		if enc[0] != 0 || enc[len(enc)-1] != 0 || bytes.IndexByte(enc[1:len(enc)-1], 0) >= 0 {
			t.Fatalf("n=%d: encoding not delimited correctly: % X", n, enc)
		}
		dec, err := Decode(enc[1 : len(enc)-1])
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if !bytes.Equal(dec, src) {
			t.Fatalf("n=%d: round trip mismatch", n)
		}
	}
}

func TestStreamDecoderChunking(t *testing.T) {
	a := []byte("first frame \x00 with a zero")
	b := bytes.Repeat([]byte{0x07, 0x00}, 200)
	c := []byte{0x00}
	stream := append(append(Encode(a), Encode(b)...), Encode(c)...)

	// Every chunk size, including frames ending and starting inside one chunk.
	for size := 1; size <= len(stream); size++ {
		var d StreamDecoder
		var got [][]byte
		for off := 0; off < len(stream); off += size {
			end := min(off+size, len(stream))
			frames, err := d.Feed(stream[off:end])
			if err != nil {
				t.Fatalf("size %d: %v", size, err)
			}
			got = append(got, frames...)
		}
		if len(got) != 3 || !bytes.Equal(got[0], a) || !bytes.Equal(got[1], b) || !bytes.Equal(got[2], c) {
			t.Fatalf("size %d: got %d frames", size, len(got))
		}
		if d.Buffered() != 0 {
			t.Fatalf("size %d: %d bytes left buffered", size, d.Buffered())
		}
	}
}

func TestStreamDecoderRecoversFromGarbage(t *testing.T) {
	var d StreamDecoder
	good := []byte{0x10, 0x20, 0x30}
	stream := append([]byte{0x00, 0x09, 0x01, 0x00}, Encode(good)...) // a truncated segment, then a valid frame
	frames, err := d.Feed(stream)
	if err == nil {
		t.Fatal("want an error for the bad segment")
	}
	if len(frames) != 1 || !bytes.Equal(frames[0], good) {
		t.Fatalf("frames = %X, want the valid frame after the bad one", frames)
	}
}

func TestStreamDecoderMaxFrame(t *testing.T) {
	d := StreamDecoder{MaxFrame: 8}
	if _, err := d.Feed([]byte{0x00, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}); err == nil {
		t.Fatal("want an error when the frame exceeds MaxFrame")
	}
	if d.Buffered() != 0 {
		t.Fatalf("oversized frame not dropped: %d bytes buffered", d.Buffered())
	}
	frames, err := d.Feed(Encode([]byte{0xAA}))
	if err != nil || len(frames) != 1 {
		t.Fatalf("decoder did not recover: frames=%v err=%v", frames, err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(mustHexF(gbEncoded))
	f.Add([]byte{0x00, 0x01, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		var d StreamDecoder
		frames, _ := d.Feed(data)
		for _, fr := range frames {
			enc := Encode(fr)
			dec, err := Decode(enc[1 : len(enc)-1])
			if err != nil || !bytes.Equal(dec, fr) {
				t.Fatalf("re-encode round trip failed for % X", fr)
			}
		}
	})
}

func mustHexF(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}
