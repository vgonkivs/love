package codec

import (
	"bytes"
	"testing"

	"gocv.io/x/gocv"
)

func TestDecodeNextFrame_EmptyData(t *testing.T) {
	frame, consumed := DecodeNextFrame([]byte{})
	if frame != nil {
		t.Error("expected nil frame for empty data")
	}
	if consumed != 0 {
		t.Errorf("expected 0 consumed, got %d", consumed)
	}
}

func TestDecodeNextFrame_TooShort(t *testing.T) {
	// Less than 4 bytes
	frame, consumed := DecodeNextFrame([]byte{0xFF, 0xD8})
	if frame != nil {
		t.Error("expected nil frame for short data")
	}
	if consumed != 0 {
		t.Errorf("expected 0 consumed, got %d", consumed)
	}
}

func TestDecodeNextFrame_NoSOI(t *testing.T) {
	// No JPEG start marker
	data := []byte{0x00, 0x00, 0x00, 0x00, 0xFF, 0xD9}
	frame, consumed := DecodeNextFrame(data)
	if frame != nil {
		t.Error("expected nil frame when no SOI marker")
	}
	if consumed != 0 {
		t.Errorf("expected 0 consumed, got %d", consumed)
	}
}

func TestDecodeNextFrame_NoEOI(t *testing.T) {
	// Has start marker but no end marker
	data := []byte{0xFF, 0xD8, 0x00, 0x00, 0x00, 0x00}
	frame, consumed := DecodeNextFrame(data)
	if frame != nil {
		t.Error("expected nil frame when no EOI marker")
	}
	if consumed != 0 {
		t.Errorf("expected 0 consumed, got %d", consumed)
	}
}

func TestDecodeNextFrame_InvalidJPEG(t *testing.T) {
	// Has markers but invalid JPEG data between them
	data := []byte{0xFF, 0xD8, 0x00, 0x00, 0xFF, 0xD9}
	frame, consumed := DecodeNextFrame(data)
	// Should skip past the invalid SOI
	if frame != nil {
		t.Error("expected nil frame for invalid JPEG")
	}
	// Should consume past the SOI marker to try again
	if consumed != 2 {
		t.Errorf("expected 2 consumed for invalid JPEG, got %d", consumed)
	}
}

func TestDecodeNextFrame_ValidJPEG(t *testing.T) {
	// Create a real minimal JPEG using gocv
	img := gocv.NewMatWithSize(10, 10, gocv.MatTypeCV8UC3)
	defer img.Close()

	// Set some pixel data
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			img.SetUCharAt(i, j*3, uint8(i*25))
			img.SetUCharAt(i, j*3+1, uint8(j*25))
			img.SetUCharAt(i, j*3+2, 128)
		}
	}

	buf, err := gocv.IMEncode(".jpg", img)
	if err != nil {
		t.Fatalf("failed to encode test image: %v", err)
	}
	defer buf.Close()

	jpegData := buf.GetBytes()

	// Test decoding
	frame, consumed := DecodeNextFrame(jpegData)
	if frame == nil {
		t.Fatal("expected non-nil frame for valid JPEG")
	}
	defer frame.Close()

	if consumed != len(jpegData) {
		t.Errorf("expected %d consumed, got %d", len(jpegData), consumed)
	}

	if frame.Empty() {
		t.Error("decoded frame should not be empty")
	}
}

func TestDecodeNextFrame_MultipleFrames(t *testing.T) {
	// Create two valid JPEGs
	img1 := gocv.NewMatWithSize(8, 8, gocv.MatTypeCV8UC3)
	defer img1.Close()
	img2 := gocv.NewMatWithSize(8, 8, gocv.MatTypeCV8UC3)
	defer img2.Close()

	buf1, err := gocv.IMEncode(".jpg", img1)
	if err != nil {
		t.Fatalf("failed to encode image 1: %v", err)
	}
	defer buf1.Close()

	buf2, err := gocv.IMEncode(".jpg", img2)
	if err != nil {
		t.Fatalf("failed to encode image 2: %v", err)
	}
	defer buf2.Close()

	// Concatenate both JPEGs
	combined := append(buf1.GetBytes(), buf2.GetBytes()...)

	// Decode first frame
	frame1, consumed1 := DecodeNextFrame(combined)
	if frame1 == nil {
		t.Fatal("expected non-nil first frame")
	}
	frame1.Close()

	if consumed1 != len(buf1.GetBytes()) {
		t.Errorf("first frame: expected %d consumed, got %d", len(buf1.GetBytes()), consumed1)
	}

	// Decode second frame from remaining data
	remaining := combined[consumed1:]
	frame2, consumed2 := DecodeNextFrame(remaining)
	if frame2 == nil {
		t.Fatal("expected non-nil second frame")
	}
	frame2.Close()

	if consumed2 != len(buf2.GetBytes()) {
		t.Errorf("second frame: expected %d consumed, got %d", len(buf2.GetBytes()), consumed2)
	}
}

func TestDecodeNextFrame_WithLeadingGarbage(t *testing.T) {
	// Create a valid JPEG
	img := gocv.NewMatWithSize(8, 8, gocv.MatTypeCV8UC3)
	defer img.Close()

	buf, err := gocv.IMEncode(".jpg", img)
	if err != nil {
		t.Fatalf("failed to encode image: %v", err)
	}
	defer buf.Close()

	// Add garbage before the JPEG
	garbage := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	combined := append(garbage, buf.GetBytes()...)

	frame, consumed := DecodeNextFrame(combined)
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}
	defer frame.Close()

	// Should consume garbage + JPEG
	expectedConsumed := len(garbage) + len(buf.GetBytes())
	if consumed != expectedConsumed {
		t.Errorf("expected %d consumed, got %d", expectedConsumed, consumed)
	}
}

func TestJPEGMarkers(t *testing.T) {
	// Verify the JPEG markers are correct
	if !bytes.Equal(jpegSOI, []byte{0xFF, 0xD8}) {
		t.Error("jpegSOI marker is incorrect")
	}
	if !bytes.Equal(jpegEOI, []byte{0xFF, 0xD9}) {
		t.Error("jpegEOI marker is incorrect")
	}
}
