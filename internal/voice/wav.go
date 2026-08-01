// Package voice lê áudio WAV e calcula perfil acústico de falante
// (F0, HNR, centroide espectral) para classificar sexo+idade e validar
// contra o roteiro (o "bandaid" anti-confusão).
package voice

import (
	"encoding/binary"
	"fmt"
	"os"
)

// ReadMono lê um WAV PCM16 (mono ou stereo) e devolve mono float64 [-1,1].
func ReadMono(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var hdr [12]byte
	if _, err := f.Read(hdr[:]); err != nil {
		return nil, fmt.Errorf("wav: cabeçalho: %w", err)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return nil, fmt.Errorf("wav: não é RIFF/WAVE")
	}

	var audioFormat, channels, bits uint16
	var sampleRate uint32
	var data []byte

	buf := make([]byte, 8)
	for {
		if _, err := f.Read(buf); err != nil {
			return nil, fmt.Errorf("wav: sem chunk data")
		}
		chunkID := string(buf[0:4])
		size := binary.LittleEndian.Uint32(buf[4:8])
		switch chunkID {
		case "fmt ":
			fb := make([]byte, size)
			if _, err := f.Read(fb); err != nil {
				return nil, err
			}
			audioFormat = binary.LittleEndian.Uint16(fb[0:2])
			channels = binary.LittleEndian.Uint16(fb[2:4])
			sampleRate = binary.LittleEndian.Uint32(fb[4:8])
			bits = binary.LittleEndian.Uint16(fb[14:16])
		case "data":
			data = make([]byte, size)
			if _, err := f.Read(data); err != nil {
				return nil, err
			}
		default:
			// pula chunk desconhecido (alinhado a par)
			skip := int64(size)
			if size%2 == 1 {
				skip++
			}
			if _, err := f.Seek(skip, 1); err != nil {
				return nil, err
			}
		}
		if data != nil {
			break
		}
	}

	if audioFormat != 1 && audioFormat != 0xFFFE { // PCM / extensível
		return nil, fmt.Errorf("wav: formato não-PCM (%d)", audioFormat)
	}
	if bits != 16 {
		return nil, fmt.Errorf("wav: suportado apenas 16 bits (tem %d)", bits)
	}
	if sampleRate != 16000 {
		return nil, fmt.Errorf("wav: esperava 16 kHz (tem %d) — converta com ffmpeg antes", sampleRate)
	}
	if channels == 0 {
		return nil, fmt.Errorf("wav: sem canais")
	}

	nSamples := len(data) / 2 / int(channels)
	out := make([]float64, nSamples)
	for i := 0; i < nSamples; i++ {
		var sum int64
		for c := 0; c < int(channels); c++ {
			idx := (i*int(channels) + c) * 2
			v := int16(binary.LittleEndian.Uint16(data[idx : idx+2]))
			sum += int64(v)
		}
		out[i] = float64(sum) / float64(int64(channels)*32768.0)
	}
	return out, nil
}
