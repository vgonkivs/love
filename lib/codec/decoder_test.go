package codec

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gocv.io/x/gocv"
	"testing"
)

func TestDecodeNextFrame_EmptyData(t *testing.T) {
	frame, consumed := DecodeNextFrame([]byte{})
	require.Nil(t, frame)
	assert.Equal(t, consumed, 0)
}

func TestDecodeNextFrame_TooShort(t *testing.T) {
	// Less than 4 bytes
	frame, consumed := DecodeNextFrame([]byte{0xFF, 0xD8})
	require.Nil(t, frame)
	assert.Equal(t, consumed, 0)
}

func TestDecodeNextFrame_NoSOI(t *testing.T) {
	// No JPEG start marker
	data := []byte{0x00, 0x00, 0x00, 0x00, 0xFF, 0xD9}
	frame, consumed := DecodeNextFrame(data)
	require.Nil(t, frame)
	assert.Equal(t, consumed, 0)
}

func TestDecodeNextFrame_NoEOI(t *testing.T) {
	// Has start marker but no end marker
	data := []byte{0xFF, 0xD8, 0x00, 0x00, 0x00, 0x00}
	frame, consumed := DecodeNextFrame(data)
	require.Nil(t, frame)
	assert.Equal(t, consumed, 0)
}

func TestDecodeNextFrame_InvalidJPEG(t *testing.T) {
	// Has markers but invalid JPEG data between them
	data := []byte{0xFF, 0xD8, 0x00, 0x00, 0xFF, 0xD9}
	frame, consumed := DecodeNextFrame(data)
	require.Nil(t, frame)
	assert.Equal(t, consumed, 2)
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
	require.Nil(t, err)
	defer buf.Close()

	jpegData := buf.GetBytes()

	// Test decoding
	frame, consumed := DecodeNextFrame(jpegData)
	require.NotNil(t, frame)
	defer frame.Close()
	assert.Equal(t, consumed, len(jpegData))
	assert.False(t, frame.Empty())
}

func TestDecodeNextFrame_MultipleFrames(t *testing.T) {
	// Create two valid JPEGs
	img1 := gocv.NewMatWithSize(8, 8, gocv.MatTypeCV8UC3)
	defer img1.Close()
	img2 := gocv.NewMatWithSize(8, 8, gocv.MatTypeCV8UC3)
	defer img2.Close()

	buf1, err := gocv.IMEncode(".jpg", img1)
	require.NoError(t, err)
	defer buf1.Close()

	buf2, err := gocv.IMEncode(".jpg", img2)
	require.NoError(t, err)
	defer buf2.Close()

	// Concatenate both JPEGs
	combined := append(buf1.GetBytes(), buf2.GetBytes()...)

	// Decode first frame
	frame1, consumed1 := DecodeNextFrame(combined)
	require.NotNil(t, frame1)
	frame1.Close()
	assert.Equal(t, consumed1, len(buf1.GetBytes()))

	// Decode second frame from remaining data
	remaining := combined[consumed1:]
	frame2, consumed2 := DecodeNextFrame(remaining)
	require.NotNil(t, frame2)
	frame2.Close()
	assert.Equal(t, consumed2, len(buf2.GetBytes()))
}

func TestDecodeNextFrame_WithLeadingGarbage(t *testing.T) {
	// Create a valid JPEG
	img := gocv.NewMatWithSize(8, 8, gocv.MatTypeCV8UC3)
	defer img.Close()

	buf, err := gocv.IMEncode(".jpg", img)
	require.NoError(t, err)
	defer buf.Close()

	// Add garbage before the JPEG
	garbage := []byte{0x00, 0x01, 0x02, 0x03, 0x04}
	combined := append(garbage, buf.GetBytes()...)

	frame, consumed := DecodeNextFrame(combined)
	assert.NotNil(t, frame)
	defer frame.Close()

	// Should consume garbage + JPEG
	expectedConsumed := len(garbage) + len(buf.GetBytes())
	assert.Equal(t, consumed, expectedConsumed)
}
