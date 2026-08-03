// Execução do pipeline completo (estágios e medição de tempo).
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"anime_dub/internal/ffmpeg"
	"anime_dub/internal/report"
	"anime_dub/internal/srt"
	"anime_dub/internal/timeline"
	"anime_dub/internal/translate"
	"anime_dub/internal/voice"
)

// Run executa o pipeline completo.
func (o *Options) Run(ctx context.Context) error {
	o.defaults()
	os.MkdirAll(o.Work, 0o755)
	os.MkdirAll(filepath.Join(o.Work, "dubs"), 0o755)
	os.MkdirAll(filepath.Join(o.Work, "refs"), 0o755)
	os.MkdirAll(filepath.Dir(o.Out), 0o755)

	if _, err := ffmpeg.Bin("ffmpeg"); err != nil {
		return err
	}
	if _, err := os.Stat(o.PyBin); err != nil {
		return fmt.Errorf("venv não encontrado em %s — rode: python3 -m venv .venv && .venv/bin/pip install -r requirements.txt", o.PyBin)
	}

	r := &report.Report{
		Input:      o.Input,
		Model:      o.Whisper,
		Separator:  o.Separator,
		Out:        o.Out,
		FlagsCount: map[string]int{},
	}

	// ═══ 1. probe + extract ═══
	st := report.Begin()
	dur, hasAudio, err := ffmpeg.Probe(o.Input)
	if err != nil {
		return err
	}
	if !hasAudio {
		return errors.New("entrada sem trilha de áudio")
	}
	r.EpDuration = dur
	o.logf("episódio: %.0f s (%.1f min)", dur, dur/60)

	if !cached(filepath.Join(o.Work, "audio16k.wav"), o.Force) ||
		!cached(filepath.Join(o.Work, "audio48k.wav"), o.Force) {
		if err := ffmpeg.Extract(o.Input,
			filepath.Join(o.Work, "audio16k.wav"),
			filepath.Join(o.Work, "audio48k.wav"), o.Threads); err != nil {
			return err
		}
	}
	r.Add("probe+extract", st.Elapsed())

	// ═══ 2. separação voz × fundo ═══
	st = report.Begin()
	vocals := filepath.Join(o.Work, "audio16k.wav")
	useSep := o.Separator == "demucs"
	vocalsRaw := filepath.Join(o.Work, "vocals.wav")

	asrErr := make(chan error, 1)
	if o.FastASR {
		go func() { asrErr <- o.stageASR(ctx, vocals) }()
	}
	if useSep {
		if !cached(vocalsRaw, o.Force) || !cached(filepath.Join(o.Work, "nv.wav"), o.Force) {
			o.logf("[sep] separando voz × fundo (demucs htdemucs, cpu)...")
			if err := o.runPy("workers/sep.py",
				"--audio", filepath.Join(o.Work, "audio48k.wav"),
				"--vocals", vocalsRaw,
				"--bg", filepath.Join(o.Work, "nv.wav")); err != nil {
				o.logf("[sep] falhou, usando original como fundo: %v", err)
			}
		}
	}
	if o.FastASR {
		if err := <-asrErr; err != nil {
			return err
		}
	} else {
		asrSrc := vocals
		if useSep && cached(vocalsRaw, false) {
			asrSrc = vocalsRaw
		}
		if err := o.stageASR(ctx, asrSrc); err != nil {
			return err
		}
	}
	r.Add("sep+asr", st.Elapsed())

	// ═══ 2b. separação de vozes sobrepostas (SepFormer) ═══
	st = report.Begin()
	overlapSrc := vocalsRaw
	if _, err := os.Stat(overlapSrc); err != nil {
		overlapSrc = vocals
	}
	if err := o.stageOverlap(ctx, overlapSrc); err != nil {
		o.logf("[overlap] falhou, mantendo segmentos originais: %v", err)
	}
	r.Add("overlap", st.Elapsed())

	// ═══ 3. diarização ═══
	st = report.Begin()
	if err := o.stageDiar(ctx); err != nil {
		return err
	}
	r.Add("diarização", st.Elapsed())

	// ═══ 4. referências de voz + perfil acústico ═══
	st = report.Begin()
	spkMap, err := o.stageSpeakers(ctx, vocalsRaw, useSep)
	if err != nil {
		return err
	}
	r.Add("refs+perfil de voz", st.Elapsed())

	// ═══ 5. roteiro + validação voz×roteiro ═══
	st = report.Begin()
	conflicts := o.stageScriptValidate(spkMap)
	for _, c := range conflicts {
		r.Conflicts = append(r.Conflicts, c)
	}
	r.Add("validação de roteiro", st.Elapsed())

	// ═══ 6. tradução ja→pt ═══
	st = report.Begin()
	lines, err := o.stageTranslate(ctx, spkMap)
	if err != nil {
		return err
	}
	r.Add("tradução", st.Elapsed())

	// ═══ 6b. emoção por fala (naturalidade de dublagem) ═══
	st = report.Begin()
	if err := o.stageEmotion(); err != nil {
		o.logf("[emotion] falhou, usando neutral: %v", err)
	}
	r.Add("emoção por fala", st.Elapsed())

	// ═══ 7. TTS (OmniVoice) ═══
	st = report.Begin()
	if err := o.stageTTS(lines, spkMap); err != nil {
		return err
	}
	r.Add("TTS OmniVoice", st.Elapsed())

	// ═══ 8. timeline + mix ═══
	st = report.Begin()
	if err := o.stageMix(ctx, dur, lines); err != nil {
		return err
	}
	r.Add("mix+mux", st.Elapsed())

	// ═══ 9. relatório ═══
	r.NLines = len(lines)
	r.TotalLines = o.TotalLines
	r.MaxLines = o.MaxLines
	r.NSpeakers = len(spkMap)
	flags := map[string]int{}
	for _, ln := range lines {
		for _, f := range ln.Flags {
			flags[f]++
		}
	}
	r.FlagsCount = flags
	r.ComputeTotals()

	repPath := filepath.Join(o.Work, "report.json")
	if err := r.Write(repPath); err != nil {
		return err
	}

	o.logf("\n═════════ RESUMO ═════════")
	o.logf("  falas dubladas : %d", len(lines))
	o.logf("  personagens    : %d", len(spkMap))
	o.logf("  saída          : %s", o.Out)
	o.logf("  tempo total    : %.0f s (%.1f min)", r.Total, r.Total/60)
	o.logf("  est. 24 min    : %.0f min", r.Est24minMin)
	if len(r.Conflicts) > 0 {
		o.logf("  ⚠ conflitos voz×roteiro: %d", len(r.Conflicts))
		for _, c := range r.Conflicts {
			o.logf("    - %s", c)
		}
	}
	o.logf("════════════════════════")
	return nil
}

// stageASR roda o faster-whisper.
func (o *Options) stageASR(ctx context.Context, audio string) error {
	out := filepath.Join(o.Work, "asr.json")
	if cached(out, o.Force) {
		return nil
	}
	o.logf("[asr] transcrevendo (%s, %s, cpu)...", o.Whisper, o.Lang)
	return o.runPy("workers/asr.py",
		"--audio", audio,
		"--lang", o.Lang,
		"--model", o.Whisper,
		"--out", out)
}

// stageOverlap separa segmentos com 2 vozes simultâneas (SepFormer).
// Substitui em asr.json os segmentos mistos pelas falas de cada voz.
type overlapSplit struct {
	OrigID int       `json:"orig_id"`
	Parts  []Segment `json:"parts"`
}
type overlapResult struct {
	Splits []overlapSplit `json:"splits"`
}

func (o *Options) stageOverlap(ctx context.Context, audio string) error {
	out := filepath.Join(o.Work, "overlap.json")
	if !cached(out, o.Force) {
		if !o.Overlap {
			return writeJSON(out, overlapResult{})
		}
		o.logf("[overlap] procurando falas com 2 vozes simultâneas (SepFormer)...")
		if err := o.runPy("workers/overlap.py",
			"--audio", audio,
			"--segments", filepath.Join(o.Work, "asr.json"),
			"--out", out,
			"--model", o.Whisper,
			"--lang", o.Lang); err != nil {
			return err
		}
	}
	ov, err := readJSON[overlapResult](out)
	if err != nil {
		return err
	}
	if len(ov.Splits) == 0 {
		return nil
	}

	asrPath := filepath.Join(o.Work, "asr.json")
	asr, err := readJSON[ASR](asrPath)
	if err != nil {
		return err
	}
	// idempotência: só aplica o split se o segmento original ainda existir
	existing := map[int]bool{}
	for _, s := range asr.Segments {
		existing[s.ID] = true
	}
	drop := map[int]bool{}
	var add []Segment
	changed := false
	for _, sp := range ov.Splits {
		if !existing[sp.OrigID] {
			continue // já aplicado em execução anterior
		}
		changed = true
		drop[sp.OrigID] = true
		add = append(add, sp.Parts...)
	}
	if !changed {
		return nil
	}
	keep := make([]Segment, 0, len(asr.Segments))
	for _, s := range asr.Segments {
		if drop[s.ID] {
			continue
		}
		keep = append(keep, s)
	}
	asr.Segments = append(keep, add...)
	sort.SliceStable(asr.Segments, func(i, j int) bool {
		return asr.Segments[i].Start < asr.Segments[j].Start
	})
	if err := writeJSON(asrPath, asr); err != nil {
		return err
	}

	// estágios derivados ficam inválidos com os novos segmentos
	for _, p := range []string{"diar.json", "emotion.json", "lines.json", "tts_times.json"} {
		os.Remove(filepath.Join(o.Work, p))
	}
	os.RemoveAll(filepath.Join(o.Work, "refs"))
	os.RemoveAll(filepath.Join(o.Work, "dubs"))
	os.MkdirAll(filepath.Join(o.Work, "refs"), 0o755)
	os.MkdirAll(filepath.Join(o.Work, "dubs"), 0o755)
	o.logf("[overlap] %d segmento(s) mistos divididos em %d falas",
		len(ov.Splits), len(add))
	return nil
}

// stageDiar agrupa falas por personagem (clusters).
func (o *Options) stageDiar(ctx context.Context) error {
	out := filepath.Join(o.Work, "diar.json")
	if cached(out, o.Force) {
		return nil
	}
	src := filepath.Join(o.Work, "vocals.wav")
	if _, err := os.Stat(src); err != nil {
		src = filepath.Join(o.Work, "audio16k.wav")
	}
	o.logf("[diar] agrupando falantes por voz...")
	if err := o.runPy("workers/diar.py",
		"--audio", src,
		"--segments", filepath.Join(o.Work, "asr.json"),
		"--out", out); err != nil {
		o.logf("[diar] falhou, tratando como 1 falante: %v", err)
		return writeJSON(out, Diar{Method: "single", NClusters: 1, Assign: map[int]int{}})
	}
	return nil
}

// stageSpeakers monta refs por personagem e calcula o perfil acústico.
func (o *Options) stageSpeakers(ctx context.Context, vocalsRaw string, useSep bool) (map[string]*Speaker, error) {
	asr, err := readJSON[ASR](filepath.Join(o.Work, "asr.json"))
	if err != nil {
		return nil, err
	}
	diar, err := readJSON[Diar](filepath.Join(o.Work, "diar.json"))
	if err != nil {
		return nil, err
	}

	// segmentos → cluster
	byCluster := map[int][]Segment{}
	for _, s := range asr.Segments {
		c := diar.Assign[s.ID]
		if diar.NClusters == 1 && len(diar.Assign) == 0 {
			c = 0
		}
		byCluster[c] = append(byCluster[c], s)
	}

	src := filepath.Join(o.Work, "vocals.wav")
	if _, err := os.Stat(src); err != nil || !useSep {
		src = filepath.Join(o.Work, "audio16k.wav")
	}

	spkMap := map[string]*Speaker{}
	clusterIDs := make([]int, 0, len(byCluster))
	for c := range byCluster {
		clusterIDs = append(clusterIDs, c)
	}
	sort.Ints(clusterIDs)

	for _, c := range clusterIDs {
		segs := byCluster[c]
		label := "spk_" + strconv.Itoa(c)
		sp := &Speaker{Cluster: c, Label: label}

		// seleciona trechos de voz para referência (6-9s, prioridade por prob×len)
		sort.SliceStable(segs, func(i, j int) bool {
			wi := (segs[i].Prob) * (segs[i].End - segs[i].Start)
			wj := (segs[j].Prob) * (segs[j].End - segs[j].Start)
			return wi > wj
		})
		var parts []string
		var txt []string
		total := 0.0
		for _, s := range segs {
			if total >= 9 {
				break
			}
			if len(parts) >= 6 {
				break
			}
			if s.End-s.Start < 0.4 {
				continue
			}
			p := filepath.Join(o.Work, "refs", fmt.Sprintf("%s_%03d.wav", label, len(parts)))
			if err := ffmpeg.Slice(src, p, s.Start, s.End); err != nil {
				continue
			}
			parts = append(parts, p)
			txt = append(txt, s.Text)
			total += s.End - s.Start
		}
		if len(parts) == 0 {
			o.logf("[profile] %s: sem trechos de voz (cluster %d, %d falas)", label, c, len(segs))
			sp.Profile = &voice.Profile{Role: voice.RoleUnknown}
			for _, s := range segs {
				sp.SegIDs = append(sp.SegIDs, s.ID)
			}
			spkMap[label] = sp
			continue
		}
		ref := filepath.Join(o.Work, "refs", label+".wav")
		if err := ffmpeg.Concat(parts, ref); err != nil {
			return nil, err
		}
		for _, p := range parts {
			os.Remove(p)
		}
		sp.RefAudio = ref
		sp.RefText = strings.Join(txt, " ")
		for _, s := range segs {
			sp.SegIDs = append(sp.SegIDs, s.ID)
		}

		prof, err := voice.AnalyzeFile(ref)
		if err != nil {
			o.logf("[profile] %s: %v", label, err)
			sp.Profile = &voice.Profile{Role: voice.RoleUnknown}
		} else {
			sp.Profile = prof
		}
		spkMap[label] = sp
	}
	o.logf("[profile] %d personagem(ns) identificado(s)", len(spkMap))
	return spkMap, nil
}

// stageScriptValidate cruza o perfil de voz com o roteiro (bandaid).
func (o *Options) stageScriptValidate(spkMap map[string]*Speaker) []string {
	asr, err := readJSON[ASR](filepath.Join(o.Work, "asr.json"))
	if err != nil {
		return nil
	}
	var cues []srt.Cue
	if o.Script != "" {
		cues, err = srt.Parse(o.Script)
		if err != nil {
			fmt.Printf("[script] erro ao ler roteiro %s: %v\n", o.Script, err)
			cues = nil
		}
	}
	roles := voice.ParseRoles(o.Roles)

	// associa falas (ASR) ao personagem do roteiro por sobreposição de tempo
	segToChar := map[int]string{}
	for _, cue := range cues {
		if cue.Speaker == "" {
			continue
		}
		for _, s := range asr.Segments {
			if overlap(s.Start, s.End, cue.Start, cue.End) > 0.3 {
				segToChar[s.ID] = cue.Speaker
			}
		}
	}

	// voto por cluster → personagem majoritário
	spkToChar := map[string]string{}
	votes := map[string]map[string]int{}
	for label, sp := range spkMap {
		votes[label] = map[string]int{}
		for _, sid := range sp.SegIDs {
			if ch := segToChar[sid]; ch != "" {
				votes[label][ch]++
			}
		}
		best, n := "", 0
		for ch, k := range votes[label] {
			if k > n {
				best, n = ch, k
			}
		}
		spkToChar[label] = best
	}

	var conflicts []string
	for label, sp := range spkMap {
		sp.Character = spkToChar[label]
		if sp.Character != "" {
			sp.Expected = roles[sp.Character]
		}
		if sp.Expected == voice.RoleUnknown || sp.Expected == "" || sp.Profile == nil {
			continue
		}
		conflict, msg := voice.CheckConflict(sp.Profile.Role, sp.Expected, sp.Profile.Conf)
		if conflict {
			sp.Conflict = true
			sp.ConflictMsg = msg
			conflicts = append(conflicts, fmt.Sprintf("%s (%s): %s", label, sp.Character, msg))
		}
	}
	if len(conflicts) > 0 {
		o.logf("[script] %d conflito(s) de voz×roteiro", len(conflicts))
	}
	return conflicts
}

// stageTranslate traduz as falas ja→pt e monta lines.json.
func (o *Options) stageTranslate(ctx context.Context, spkMap map[string]*Speaker) ([]Line, error) {
	asr, err := readJSON[ASR](filepath.Join(o.Work, "asr.json"))
	if err != nil {
		return nil, err
	}
	diar, err := readJSON[Diar](filepath.Join(o.Work, "diar.json"))
	if err != nil {
		return nil, err
	}

	// filtra falas com voz
	var sel []Segment
	idxOf := map[int]int{}
	for _, s := range asr.Segments {
		if s.NoSpeech > 0.9 || strings.TrimSpace(s.Text) == "" {
			continue
		}
		idxOf[s.ID] = len(sel)
		sel = append(sel, s)
	}
	o.logf("[translate] %d falas %s→pt...", len(sel), o.Lang)
	o.TotalLines = len(sel)
	texts := make([]string, len(sel))
	for i, s := range sel {
		texts[i] = s.Text
	}
	tr := translate.New()
	tr.Workers = o.Threads / 2
	if tr.Workers < 2 {
		tr.Workers = 2
	}
	if tr.Workers > 8 {
		tr.Workers = 8
	}
	pts, err := tr.Translate(ctx, o.Lang, "pt", texts)
	nmiss := 0
	for i := range pts {
		if pts[i] == "" {
			pts[i] = texts[i]
			nmiss++
		}
	}
	if nmiss > 0 {
		o.logf("[translate] %d/%d falas falharam (%v) — mantidas no original", nmiss, len(texts), err)
	}

	var lines []Line
	for i, s := range sel {
		c := diar.Assign[s.ID]
		if diar.NClusters == 1 && len(diar.Assign) == 0 {
			c = 0
		}
		label := "spk_" + strconv.Itoa(c)
		sp := spkMap[label]
		role := voice.RoleUnknown
		gender, age := "", ""
		if sp != nil && sp.Profile != nil {
			role = sp.Profile.Role
			gender = sp.Profile.Gender
			age = sp.Profile.Age
		}
		lines = append(lines, Line{
			ID:      s.ID,
			Start:   s.Start,
			End:     s.End,
			JA:      s.Text,
			PT:      pts[i],
			Speaker: label,
			Gender:  gender,
			Age:     age,
			Role:    role.RolePT(),
		})
	}
	if o.MaxLines > 0 && len(lines) > o.MaxLines {
		lines = lines[:o.MaxLines]
	}
	return lines, nil
}

// stageEmotion classifica a emoção de cada fala original (f0+energia+ritmo).
// Alimenta o TTS com tags de atuação (dublagem natural tipo dublador).
func (o *Options) stageEmotion() error {
	out := filepath.Join(o.Work, "emotion.json")
	if cached(out, o.Force) {
		return nil
	}
	src := filepath.Join(o.Work, "vocals.wav")
	if _, err := os.Stat(src); err != nil {
		src = filepath.Join(o.Work, "audio16k.wav")
	}
	o.logf("[emotion] classificando emoção por fala (f0+energia+ritmo)...")
	if err := o.runPy("workers/emotion.py",
		"--audio", src,
		"--segments", filepath.Join(o.Work, "asr.json"),
		"--out", out); err != nil {
		return err
	}
	return nil
}

// stageTTS gera as falas dubladas com OmniVoice (motor do aiuto_trend_producer).
func (o *Options) stageTTS(lines []Line, spkMap map[string]*Speaker) error {
	linesJSON := filepath.Join(o.Work, "lines.json")
	if !cached(linesJSON, o.Force) {
		speakers := map[string]any{}
		for label, sp := range spkMap {
			speakers[label] = map[string]string{
				"ref_audio": sp.RefAudio,
				"ref_text":  sp.RefText,
			}
		}
		type ttsLine struct {
			ID      int     `json:"id"`
			Speaker string  `json:"speaker"`
			Start   float64 `json:"start"`
			End     float64 `json:"end"`
			JA      string  `json:"ja"`
			PT      string  `json:"pt"`
		}
		tl := make([]ttsLine, len(lines))
		for i, ln := range lines {
			tl[i] = ttsLine{ID: ln.ID, Speaker: ln.Speaker, Start: ln.Start, End: ln.End, JA: ln.JA, PT: ln.PT}
		}
		if err := writeJSON(linesJSON, map[string]any{"speakers": speakers, "lines": tl}); err != nil {
			return err
		}
	}
	if cached(filepath.Join(o.Work, "tts_times.json"), o.Force) {
		// todas as falas já foram geradas
		for i := range lines {
			lines[i].DubFile = filepath.Join(o.Work, "dubs", fmt.Sprintf("%05d.wav", lines[i].ID))
			if d, err := ffmpeg.DurWav(lines[i].DubFile); err == nil {
				lines[i].DubLen = d
			}
		}
		return nil
	}
	o.logf("[tts] gerando %d falas dubladas (OmniVoice, cpu)...", len(lines))
	ttsArgs := []string{
		"workers/tts.py",
		"--lines", linesJSON,
		"--out", filepath.Join(o.Work, "dubs"),
		"--work", o.Work,
		"--engine", o.Engine,
		"--max-lines", strconv.Itoa(o.MaxLines),
	}
	if o.TtsSpeed > 0 {
		ttsArgs = append(ttsArgs, "--speed", strconv.FormatFloat(o.TtsSpeed, 'f', 3, 64))
	}
	if err := o.runPy(ttsArgs...); err != nil {
		return err
	}
	tt, err := readJSON[ttsTimes](filepath.Join(o.Work, "tts_times.json"))
	if err == nil && tt.NErr > 0 {
		return fmt.Errorf("tts: %d fala(s) falharam — log: %s",
			tt.NErr, filepath.Join(o.Work, "tts_errors.log"))
	}
	for i := range lines {
		lines[i].DubFile = filepath.Join(o.Work, "dubs", fmt.Sprintf("%05d.wav", lines[i].ID))
		if d, err := ffmpeg.DurWav(lines[i].DubFile); err == nil {
			lines[i].DubLen = d
		}
	}
	return nil
}

// stageMix alinha as falas à janela original e monta o vídeo final.
func (o *Options) stageMix(ctx context.Context, dur float64, lines []Line) error {
	tlLines := make([]timeline.Line, len(lines))
	for i, ln := range lines {
		tlLines[i] = timeline.Line{
			ID:      ln.ID,
			Start:   ln.Start,
			End:     ln.End,
			Speaker: ln.Speaker,
			DubFile: ln.DubFile,
			DubLen:  ln.DubLen,
		}
	}
	planned := timeline.Plan(tlLines, o.StretchMax)
	for i := range lines {
		lines[i].Place = planned[i].Place
		lines[i].Atempo = planned[i].Atempo
		lines[i].PlayLen = planned[i].PlayLen
		lines[i].Flags = planned[i].Flags
	}

	// trilha de silêncio com duração total
	silence := filepath.Join(o.Work, "silence.wav")
	if !cached(silence, o.Force) {
		if err := ffmpeg.Run("-y", "-f", "lavfi",
			"-i", "anullsrc=r=48000:cl=stereo",
			"-t", strconv.FormatFloat(dur, 'f', 3, 64),
			"-c:a", "pcm_s16le", silence); err != nil {
			return err
		}
	}

	bg := filepath.Join(o.Work, "nv.wav")
	if _, err := os.Stat(bg); err != nil {
		bg = filepath.Join(o.Work, "audio48k.wav")
	}
	o.logf("[mix] mixando %d falas + ducking + loudnorm -14 LUFS...", len(planned))
	args := timeline.MixArgs(o.Input, bg, silence, o.Out, dur, planned, o.Threads)
	if err := ffmpeg.Run(args...); err != nil {
		return err
	}
	o.logf("[mix] OK → %s", o.Out)
	return nil
}

func overlap(a0, a1, b0, b1 float64) float64 {
	lo := a0
	if b0 > lo {
		lo = b0
	}
	hi := a1
	if b1 < hi {
		hi = b1
	}
	if hi < lo {
		return 0
	}
	return hi - lo
}

type ttsTimes struct {
	TotalMS int                `json:"total_ms"`
	PerLine map[string]float64 `json:"per_line_ms"`
	NOk     int                `json:"n_ok"`
	NErr    int                `json:"n_err"`
}
