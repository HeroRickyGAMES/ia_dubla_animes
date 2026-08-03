// Package timeline alinha cada fala dublada à janela de tempo da fala
// original (isocronia) ancorando o início na própria janela (anti-drift),
// estirando no máximo stretchMax quando a fala colidiria com a seguinte e
// cortando só o mínimo de cauda como último recurso (flags "clipped").
// Também gera o filtergraph ffmpeg do mix com ducking.
package timeline

import (
	"fmt"
	"math"
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
	PlayLen   float64 // duração efetiva no mix (pós atempo/atrim)
	Flags     []string
}

const (
	headPad    = 0.05 // 50ms de "respiração" antes da fala dublada
	minGap     = 0.10 // vão mínimo entre falas dubladas
	windowTol  = 0.20 // tolerância para a fala estourar a janela sem flag
	overlapTol = 0.40 // sobreposição natural máxima aceita com a fala seguinte
)

// Plan define a posição de cada fala. Política:
//  1. Ancorar no início da janela original (nunca deslocar → zero drift).
//  2. Se a fala colidir com a seguinte além de overlapTol, estira até
//     stretchMax (ex.: 0.15 = até 15%) para caber no espaço disponível.
//  3. Se ainda assim não couber, corta o mínimo de cauda ("clipped").
//     O corte nunca mexe no início, preservando a sincronia da boca.
func Plan(lines []Line, stretchMax float64) []Line {
	ls := make([]Line, len(lines))
	copy(ls, lines)
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].Start < ls[j].Start })

	for i := range ls {
		l := &ls[i]
		window := l.End - l.Start
		if window <= 0 {
			window = 1.0
		}

		// 1) anti-drift: começa sempre no início da própria janela
		place := l.Start + headPad
		eff := l.DubLen
		atemp := 1.0

		if l.DubLen > window+windowTol {
			l.Flags = append(l.Flags, "long")
		}

		// limite: não passar da fala seguinte (tolerância de overlap natural)
		nextStart := math.Inf(1)
		if i+1 < len(ls) {
			nextStart = ls[i+1].Start
		}
		limit := nextStart - minGap + overlapTol
		if place+eff > limit {
			room := limit - place
			switch {
			case room <= 0:
				// janela original já sobreposta pela próxima fala
				eff = 0.05
				l.Flags = append(l.Flags, "clipped")
			case stretchMax > 0 && l.DubLen <= room*(1+stretchMax):
				atemp = l.DubLen / room
				eff = room
				l.Flags = append(l.Flags, "stretched")
			default:
				eff = room
				if eff < l.DubLen {
					l.Flags = append(l.Flags, "clipped")
				}
			}
		}
		l.Place = place
		l.Atempo = atemp
		l.PlayLen = eff
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
		// corte mínimo de cauda (linhas "clipped") sem mexer no início
		sb.WriteString(",atrim=0:" + fnum(l.PlayLen))
		sb.WriteString(",asetpts=N/SR/TB,adelay=" + strconv.Itoa(ms) + ",apad[d" + strconv.Itoa(i) + "];")
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
