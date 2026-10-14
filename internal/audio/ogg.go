package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// VorbisDuration reads the length of an Ogg Vorbis file from its headers: the
// sample rate of the identification packet and the final page's granule
// position. It decodes nothing, so a bulk import can afford it per file.
func VorbisDuration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		return 0, err
	}
	head = head[:n]
	if len(head) < 28 || string(head[:4]) != "OggS" {
		return 0, fmt.Errorf("not an Ogg file")
	}
	packet := 27 + int(head[26])
	if len(head) < packet+16 || head[packet] != 1 || string(head[packet+1:packet+7]) != "vorbis" {
		return 0, fmt.Errorf("not a Vorbis stream")
	}
	rate := binary.LittleEndian.Uint32(head[packet+12 : packet+16])
	if rate == 0 {
		return 0, fmt.Errorf("invalid Vorbis sample rate")
	}
	serial := head[14:18]
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	// An Ogg page is at most 65307 bytes, so the last one starts in this tail.
	tail := make([]byte, min(info.Size(), 2*65536))
	if _, err = f.ReadAt(tail, info.Size()-int64(len(tail))); err != nil {
		return 0, err
	}
	for i := bytes.LastIndex(tail, []byte("OggS")); i >= 0; i = bytes.LastIndex(tail[:i], []byte("OggS")) {
		if len(tail)-i < 27 || tail[i+4] != 0 || !bytes.Equal(tail[i+14:i+18], serial) {
			continue
		}
		granule := int64(binary.LittleEndian.Uint64(tail[i+6 : i+14]))
		if granule <= 0 {
			continue
		}
		return float64(granule) / float64(rate), nil
	}
	return 0, fmt.Errorf("no final Ogg page")
}
