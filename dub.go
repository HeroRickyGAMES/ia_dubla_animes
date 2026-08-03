// dub.go — CLI principal da dublagem automática de anime (ja→pt).
// Orquestração 100% Go; Python só como wrapper fino dos modelos ML
// (faster-whisper, demucs, OmniVoice) usando o venv do projeto.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"anime_dub/internal/pipeline"
)

const banner = `
╔══════════════════════════════════════════════════════╗
║   anime_dub — dublagem automática de anime (ja→pt)   ║
║   Go + OmniVoice (motor aiuto_trend_producer) · CPU  ║
╚══════════════════════════════════════════════════════╝
`

func usage() {
	fmt.Print(banner)
	fmt.Print(`
Uso:
  ./dub check                      verifica ambiente (ffmpeg, venv, modelos)
  ./dub dub [flags]                dubla um episódio de ponta a ponta

Flags de 'dub':
  -i, --input FILE      vídeo do episódio (mp4/mkv)
  -o, --out FILE        saída (default: out/final.mkv)
  -w, --work DIR        diretório intermediário (default: work)
      --model SIZE      faster-whisper: small|medium|large-v3 (default: small)
      --lang CODE       idioma original ja|en|... → dubla para pt (default: ja)
      --sep MODE        demucs|none  — separação voz×fundo (default: demucs)
      --fast-asr        roda ASR sobre o áudio original em paralelo com o demucs
      --overlap=false   NÃO separar falas com 2 vozes simultâneas (SepFormer; default on)
      --script FILE     roteiro com falantes (SRT/ASS/JSON) p/ validar voz×papel
      --roles "a:menino,b:menina"   papel esperado por personagem
      --stretch-max F   acelerar a fala até F se não couber na janela (default 0.15)
      --tts-speed F     falar mais rápido no OmniVoice (ex: 1.1; 0 = natural)
      --max-lines N     limita o nº de falas (testes rápidos)
      --force           refaz estágios já cacheados
      --threads N       threads de CPU (default: 12)
      --engine DIR      pasta do aiuto_trend_producer (motor TTS)
      -v                log detalhado dos workers

Exemplo:
  ./dub dub -i ep01.mp4 -o out/ep01.mkv --model small --script script.srt \
      --roles "naruto:menino,sakura:menina,sasuke:menino"

Saídas em work/ e docs/04-resultados.md (relatório + estimativa 24 min).
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "help", "--help", "-h":
		usage()
	case "check":
		opts := pipeline.Options{}
		fs := flag.NewFlagSet("check", flag.ExitOnError)
		fs.StringVar(&opts.Engine, "engine", "", "pasta do aiuto_trend_producer")
		fs.Parse(os.Args[2:])
		if err := opts.Check(); err != nil {
			os.Exit(1)
		}
	case "dub":
		opts := pipeline.Options{}
		fs := flag.NewFlagSet("dub", flag.ExitOnError)
		fs.StringVar(&opts.Input, "i", "", "vídeo de entrada")
		fs.StringVar(&opts.Input, "input", "", "vídeo de entrada")
		fs.StringVar(&opts.Out, "o", "out/final.mkv", "saída")
		fs.StringVar(&opts.Out, "out", "out/final.mkv", "saída")
		fs.StringVar(&opts.Work, "w", "work", "workdir")
		fs.StringVar(&opts.Work, "work", "work", "workdir")
		fs.StringVar(&opts.Whisper, "model", "small", "tamanho do faster-whisper")
		fs.StringVar(&opts.Lang, "lang", "ja", "idioma original (ja|en|...), dubla para pt")
		fs.StringVar(&opts.Separator, "sep", "demucs", "demucs|none")
		fs.BoolVar(&opts.FastASR, "fast-asr", false, "ASR paralelo sobre o original")
		fs.BoolVar(&opts.Overlap, "overlap", true, "separar falas com 2 vozes (SepFormer)")
		fs.StringVar(&opts.Script, "script", "", "roteiro p/ validar voz×papel")
		fs.StringVar(&opts.Roles, "roles", "", "papel esperado por personagem")
		fs.Float64Var(&opts.StretchMax, "stretch-max", 0.15, "acelerar até X se não couber")
		fs.Float64Var(&opts.TtsSpeed, "tts-speed", 0, "falar mais rápido no OmniVoice (ex: 1.1; 0 = natural")
		fs.IntVar(&opts.MaxLines, "max-lines", 0, "limita falas")
		fs.BoolVar(&opts.Force, "force", false, "refaz estágios cacheados")
		fs.IntVar(&opts.Threads, "threads", 12, "threads CPU")
		fs.StringVar(&opts.Engine, "engine", "", "pasta do aiuto_trend_producer")
		fs.BoolVar(&opts.Verbose, "v", false, "log detalhado")
		fs.Parse(os.Args[2:])

		if opts.Input == "" {
			fmt.Println("erro: informe o vídeo com -i/--input")
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 48*time.Hour)
		defer cancel()
		if err := opts.Run(ctx); err != nil {
			fmt.Printf("\nerro: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("subcomando desconhecido: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}
