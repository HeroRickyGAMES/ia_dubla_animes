# AGENTS.md — anime_dub

Guia para agentes de IA que forem trabalhar/consultar este projeto.

## O que é

Pipeline de **dublagem automática de anime (japonês → português)**.
Orquestrador em **Go** + camada fina **Python** (só para modelos ML).

## Regras de ouro (decisões de design — NÃO quebrar)

1. **Orquestração em Go.** Todo fluxo, temporização, mistura, tradução, perfil
   de voz e relatório é feito em Go. Python é só wrapper dos modelos:
   - `workers/asr.py` → faster-whisper (transcrição ja + timestamps)
   - `workers/sep.py` → demucs (separar voz × fundo)
   - `workers/tts.py` → OmniVoice (motor do `~/projetos/aiuto_trend_producer`)
2. **CPU only.** Nunca usar GPU. `device_map="cpu"`, `dtype=float32`.
3. **Ferramentas gratuitas.** OmniVoice (Apache-2.0), faster-whisper (MIT),
   demucs (MIT), ffmpeg, Google Translate endpoint gratuito.
4. **Respeitar a fala original**: não atropelar falas, não acelerar a fala
   dublada. O janela de tempo de cada fala vem da fala original (ASR+VAD).
5. **Voz original como base**: clonagem zero-shot por personagem com a voz
   isolada do próprio anime (via demucs `vocals`).
6. **Naturalidade da dublagem**: pausas naturais, isocronia (fala cabe no
   tempo do lábio), mix com ducking (música baixa durante a fala), loudnorm -14 LUFS.
7. **venv**: usar `.venv/` do projeto (`python3 -m venv .venv`).
   Python do venv: `3.12.3`. Go: `/home/heroricky/projetos/go/bin/go` (1.26.5).

## Estrutura

```
anime_dub/
├── dub.go               # CLI principal (subcomandos por estágio)
├── internal/
│   ├── cli/             # flags/subcomandos
│   ├── pipeline/        # orquestração, paralelismo, medição de tempo
│   ├── ffmpeg/          # wrapper ffmpeg/ffprobe
│   ├── srt/             # parser de script (SRT/ASS/texto com falantes)
│   ├── translate/       # tradução ja→pt (endpoint gratuito, batching)
│   ├── voice/           # perfil de voz (F0/spectral → sexo+idade) e validação
│   ├── timeline/        # alinhamento temporal das falas + automação de volume
│   └── report/          # relatório final + estimativa para 24 min
├── workers/             # Python thin wrappers (usam .venv)
├── docs/                # documentação (começar por docs/02-arquitetura.md)
├── requirements.txt
└── AGENTS.md
```

## Comandos

```bash
# build
/home/heroricky/projetos/go/bin/go build -o dub ./...        # do dir do projeto
# ver ajuda
./dub --help
# pipeline completo
./dub dub --input ep01.mp4 --lang ja --to pt --out out/ --model small
```

## Verificação

- `./dub check` — verifica binários (ffmpeg, go, python, venv) e importa modelos.
- Sempre rodar `go build` e `go vet` após mudanças em Go.

## Docs de trabalho

- `docs/01-pesquisa.md` — pesquisa: OmniVoice + naturalidade de dublagem
- `docs/02-arquitetura.md` — arquitetura do pipeline (datas, JSONs intermediários)
- `docs/03-progresso.md` — log cronológico do trabalho (atualizar sempre!)
- `docs/04-resultados.md` — resultados, tempos medidos, estimativa 24 min

## Dados intermediários (workdir)

```
work/
├── audio16k.wav        # mono 16 kHz p/ ASR/diarização
├── audio48k.wav        # stereo 48 kHz p/ mix
├── vocals.wav          # voz isolada (demucs)
├── nv.wav              # fundo sem voz (demucs)
├── asr.json            # transcrição ja + timestamps
├── diar.json           # clusters de falante por segmento
├── profile.json        # sexo/idade por falante + validação
├── lines.json          # falas completas (id, tempos, ja, pt, speaker)
├── dubs/<line>.wav     # falas dubladas (OmniVoice)
├── refs/<spk>.wav      # referência de voz por personagem
└── final.mkv           # vídeo dublado
```

## Estimativa 24 min (atualizar em docs/04-resultados.md)

Ver `docs/04-resultados.md`.
