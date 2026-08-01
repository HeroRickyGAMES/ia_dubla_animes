// Package timeline alinha cada fala dublada à janela de tempo da fala
// original (isocronia), sem acelerar (padrão) e sem atropelar falas.
// Também gera o filtergraph ffmpeg do mix com ducking.
package timeline

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Line é uma fala pronta para mixagem.
type Line struct {
	ID        int
	Start     float64 // janela da fala original
	End       float64
	Speaker   string
	DubFile   string
	DubLen    float64 // duração natural da fala dublada
	Place     float64 // onde começa no mix (s)
	Atempo    float64 // 1.0 = natural; >1 = acelerada (só com stretch-max)
	Flags     []string
}

const (
	headPad    = 0.05  // 50ms de "respiração" antes da fala dublada
	minGap     = 0.10  // vão mínimo entre falas dubladas
	windowTol  = 0.20  // tolerância para a fala estourar a janela sem flag
)

// Plan define a posição de cada fala respeitando a janela original.
// Regras: não atropelar (sem overlap entre falas), não acelerar
// (stretchMax padrão 0). Se a fala não cabe, sinaliza e desloca se preciso.
func Plan(lines []Line, stretchMax float64) []Line {
	ls := make([]Line, len(lines))
	copy(ls, lines)
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].Start < ls[j].Start })

	prevEnd := 0.0
	for i := range ls {
		l := &ls[i]
		window := l.End - l.Start
		if window <= 0 {
			window = 1.0
		}

		// 1) não atropelar: começa depois da fala anterior (+ gap)
		place := l.Start + headPad
		if place < prevEnd+minGap {
			place = prevEnd + minGap
			l.Flags = append(l.Flags, "shifted")
		}

		// 2) não acelerar: fala natural; se não cabe, tentar stretch máximo opcional
		atemp := 1.0
		effective := l.DubLen
		if effective > window+windowTol {
			if stretchMax > 0 {
				// quanto podemos reduzir no máximo
				factor := l.DubLen / window
				maxFactor := 1.0 + stretchMax
				if factor <= maxFactor {
					atemp = factor
					effective = l.DubLen / atemp
					l.Flags = append(l.Flags, "stretched")
				} else {
					l.Flags = append(l.Flags, "long")
				}
			} else {
				l.Flags = append(l.Flags, "long")
			}
		}
		l.Place = place
		l.Atempo = atemp
		prevEnd = place + effective
	}
	return ls
}

// MixArgs monta a linha de comando do ffmpeg para mix + mux.
// inputs[0] = vídeo, inputs[1] = fundo (bg), dubs = falas dubladas.
// silencePath = wav de silêncio com a duração do episódio.
func MixArgs(video, bg, silence, out string, dur float64, lines []Line, threads int) []string {
	var sb strings.Builder
	var as []string

	sb.WriteString("[1:a]aresample=48000,aformat=channel_layouts=stereo,")
	sb.WriteString("atrim=0:")
	sb.WriteString(fnum(dur))
	sb.WriteString(",asetpts=N/SR/TB,volume=0.9[bg];")

	// entrada de silêncio garante trilha com duração total do episódio
	sb.WriteString("[2:a]aformat=sample_rates=48000:channel_layouts=stereo,")
	sb.WriteString("asetpts=N/SR/TB[sil];")

	lineInputs := 3
	for i, l := range lines {
		idx := lineInputs + i
		ms := int(l.Place * 1000)
		atemp := fnum(l.Atempo)
		sb.WriteString(fmt.Sprintf("[%d:a]aresample=48000,aformat=channel_layouts=mono", idx))
		if l.Atempo > 1.001 {
			sb.WriteString(",atempo=" + atemp)
		}
		sb.WriteString(fmt.Sprintf(",adelay=%d,apad[d%d];", ms, i))
		as = append(as, fmt.Sprintf("[d%d]", i))
	}

	// mixa silêncio + todas as falas
	sb.WriteString("[sil]")
	for _, a := range as {
		sb.WriteString(a)
	}
	sb.WriteString(fmt.Sprintf("amix=inputs=%d:normalize=0:dropout_transition=0[dial];",
		len(as)+1))

	// ducking: fundo cede quando o diálogo fala
	// (asplit: o sinal [dial] não pode alimentar 2 filtros no ffmpeg 6.1.1)
	sb.WriteString("[dial]asplit=2[dsc][dmix];")
	sb.WriteString("[bg][dsc]sidechaincompress=" +
		"threshold=0.02:ratio=6:attack=8:release=250:makeup=1[bgd];")
	sb.WriteString("[dmix][bgd]amix=inputs=2:normalize=0," +
		"alimiter=limit=0.95,loudnorm=I=-14:TP=-1.5:LRA=11[aud]")

	filter := sb.String()
	args := []string{"-y"}
	args = append(args, "-i", video, "-i", bg, "-i", silence)
	for _, l := range lines {
		args = append(args, "-i", l.DubFile)
	}
	args = append(args,
		"-filter_complex", filter,
		"-map", "0:v", "-map", "[aud]",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "192k",
		"-metadata:s:a:0", "language=por",
		"-shortest",
	)
	if threads > 0 {
		args = append(args, "-threads", strconv.Itoa(threads))
	}
	args = append(args, out)
	return args
}

func fnum(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}
