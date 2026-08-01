// Package pipeline orquestra todos os estágios da dublagem.
// Regras: CPU only, ferramentas gratuitas, janela de tempo da fala
// original respeitada (sem atropelo, sem aceleração), voz original
// como base de clonagem, validação de voz × roteiro.
package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"anime_dub/internal/ffmpeg"
	"anime_dub/internal/voice"
)

// Options configura o pipeline.
type Options struct {
	Input      string
	Out        string
	Work       string
	Lang       string // idioma original (ja|en|...), default "ja"
	Whisper    string // small|medium|large-v3
	Separator  string // demucs|none
	FixRoles   bool
	StretchMax float64
	FastASR    bool
	Script     string
	Roles      string
	MaxLines   int
	TotalLines int // falas selecionadas para tradução (antes do corte)
	Force      bool
	Threads    int
	Engine     string // pasta do aiuto_trend_producer
	PyBin      string // caminho do python do venv
	Verbose    bool
}

func (o *Options) defaults() {
	if o.Out == "" {
		o.Out = "out/final.mkv"
	}
	// --out pode vir como diretório (ex.: "out/"): vira out/final.mkv
	if st, err := os.Stat(o.Out); err == nil && st.IsDir() ||
		strings.HasSuffix(o.Out, "/") || strings.HasSuffix(o.Out, string(filepath.Separator)) {
		o.Out = filepath.Join(o.Out, "final.mkv")
	}
	if o.Work == "" {
		o.Work = "work"
	}
	if o.Lang == "" {
		o.Lang = "ja"
	}
	if o.Whisper == "" {
		o.Whisper = "small"
	}
	if o.Separator == "" {
		o.Separator = "demucs"
	}
	if o.Threads <= 0 {
		o.Threads = 12
	}
	if o.Engine == "" {
		o.Engine = filepath.Join(os.Getenv("HOME"), "projetos", "aiuto_trend_producer")
	}
	if o.PyBin == "" {
		o.PyBin = filepath.Join(o.Work, "..", ".venv", "bin", "python")
	}
	o.PyBin, _ = filepath.Abs(o.PyBin)
}

// ───────────────────────── tipos de dados ─────────────────────────

type Word struct {
	W     string  `json:"w"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	P     float64 `json:"p"`
}

type Segment struct {
	ID       int     `json:"id"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Text     string  `json:"text"`
	Prob     float64 `json:"prob"`
	NoSpeech float64 `json:"no_speech"`
	Words    []Word  `json:"words"`
}

type ASR struct {
	Info struct {
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	} `json:"info"`
	Segments []Segment `json:"segments"`
}

type Diar struct {
	Method     string         `json:"method"`
	NClusters  int            `json:"n_clusters"`
	Silhouette float64        `json:"silhouette"`
	Assign     map[int]int    `json:"assign"`
}

type Speaker struct {
	Cluster   int
	Label     string
	RefAudio  string
	RefText   string
	SegIDs    []int
	Profile   *voice.Profile
	Character string
	Expected  voice.Role
	Conflict  bool
	ConflictMsg string
}

type Line struct {
	ID      int     `json:"id"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	JA      string  `json:"ja"`
	PT      string  `json:"pt"`
	Speaker string  `json:"speaker"`
	Gender  string  `json:"gender"`
	Age     string  `json:"age"`
	Role    string  `json:"role"`
	DubFile string  `json:"-"`
	DubLen  float64 `json:"-"`
	Place   float64 `json:"-"`
	Atempo  float64 `json:"-"`
	Flags   []string `json:"-"`
}

// ───────────────────────── helpers ─────────────────────────

func (o *Options) logf(format string, a ...any) {
	fmt.Printf("["+time.Now().Format("15:04:05")+"] "+format+"\n", a...)
}

func (o *Options) py(args ...string) *exec.Cmd {
	cmd := exec.Command(o.PyBin, args...)
	cmd.Env = append(os.Environ(),
		"OMP_NUM_THREADS="+strconv.Itoa(o.Threads),
		"TORCH_NUM_THREADS="+strconv.Itoa(o.Threads),
		"PYTHONUNBUFFERED=1",
		"HF_HUB_DISABLE_PROGRESS_BARS=0",
	)
	return cmd
}

func (o *Options) runPy(args ...string) error {
	cmd := o.py(args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := strings.TrimSpace(string(out))
		if len(tail) > 1200 {
			tail = tail[len(tail)-1200:]
		}
		return fmt.Errorf("worker %s: %v\n%s", filepath.Base(args[0]), err, tail)
	}
	if o.Verbose {
		o.logf("[worker] %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func cached(path string, force bool) bool {
	if force {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func readJSON[T any](path string) (*T, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ───────────────────────── check ─────────────────────────

// Check verifica o ambiente (binários + venv + import de modelos).
func (o *Options) Check() error {
	o.defaults()
	ok := true
	if _, err := ffmpeg.Bin("ffmpeg"); err != nil {
		fmt.Printf("[check] ffmpeg : %v\n", false)
		ok = false
	} else {
		fmt.Printf("[check] ffmpeg : %v\n", true)
	}
	if _, err := ffmpeg.Bin("ffprobe"); err != nil {
		fmt.Printf("[check] ffprobe: %v\n", false)
		ok = false
	} else {
		fmt.Printf("[check] ffprobe: %v\n", true)
	}
	if _, err := os.Stat(o.PyBin); err != nil {
		fmt.Printf("[check] venv python: %v (%s)\n", false, o.PyBin)
		ok = false
	} else {
		fmt.Printf("[check] venv python: %v (%s)\n", true, o.PyBin)
	}
	if _, err := os.Stat(filepath.Join(o.Engine, "modules", "omnivoice_narrator.py")); err != nil {
		fmt.Printf("[check] engine aiuto_trend_producer: %v (%s)\n", false, o.Engine)
		ok = false
	} else {
		fmt.Printf("[check] engine aiuto_trend_producer: %v\n", true)
	}

	probe := `
mods = ["torch", "faster_whisper", "omnivoice", "numpy", "soundfile", "pydub"]
import importlib
missing = [m for m in mods if importlib.util.find_spec(m) is None]
try:
    import torch
    gpu = torch.cuda.is_available()
except Exception:
    gpu = False
print("missing=", ",".join(missing))
print("gpu_available=", gpu)
`
	cmd := o.py("-c", probe)
	out, _ := cmd.CombinedOutput()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, ln := range lines {
		if strings.HasPrefix(ln, "missing=") {
			miss := strings.TrimPrefix(ln, "missing=")
			if miss == "" {
				fmt.Printf("[check] modelos python: todos presentes\n")
			} else {
				fmt.Printf("[check] FALTANDO no venv: %s\n", miss)
				ok = false
			}
		}
		if strings.HasPrefix(ln, "gpu_available=") {
			fmt.Printf("[check] gpu: %s (pipeline roda em CPU)\n", strings.TrimPrefix(ln, "gpu_available="))
		}
	}
	if ok {
		fmt.Println("[check] ambiente OK")
	} else {
		fmt.Println("[check] ambiente com problemas — execute: .venv/bin/pip install -r requirements.txt")
		return fmt.Errorf("check falhou")
	}
	return nil
}
