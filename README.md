# anime_dub

Pipeline de **dublagem automática de anime (japonês/inglês → português)**.

Orquestrador em **Go** + camada fina **Python** (só modelos ML). CPU only.

## Como usar

```bash
# build
/home/heroricky/projetos/go/bin/go build -o dub ./...

# ver ajuda
./dub --help

# pipeline completo
./dub dub --input ep01.mp4 --lang ja --to pt --out out/ --model small
```

Flag `--lang` define o idioma ORIGINAL (ja|en|...); a dublagem é sempre para
**pt**. Em áudio em inglês use `--lang en` (o ja força alucinação de kanji).

## Estágios

1. **probe+extract** (ffmpeg) → `audio16k.wav` (ASR) + `audio48k.wav` (mix)
2. **sep** (demucs) → voz isolada `vocals.wav` + fundo `nv.wav`
3. **asr** (faster-whisper) → `asr.json` (texto + timestamps)
4. **overlap** (SepFormer) → separa segmentos com 2 vozes simultâneas
5. **diar** (ECAPA/MFCC) → cluster de falante por segmento (`diar.json`)
6. **speakers** → refs de voz por personagem + perfil acústico
   (F0/spectral → sexo+idade)
7. **translate** → ja/en→pt (endpoint gratuito, batching, fallback por fala)
8. **emotion** → tag de atuação por fala (naturalidade de dublagem)
9. **tts** (OmniVoice) → `dubs/<line>.wav` com clone zero-shot da voz do anime
10. **timeline+mix** → isocronia (fala cabe na janela do lábio), ducking,
    loudnorm -14 LUFS → `final.mkv`

## Dados intermediários

Tudo em `work/` (ver AGENTS.md). Estágios são cacheados por arquivo — rodar
de novo retoma do ponto que parou.

## Verificação

```bash
./dub check   # binários + venv + importação de modelos
```

## Docs

- `docs/01-pesquisa.md` — pesquisa OmniVoice + naturalidade de dublagem
- `docs/02-arquitetura.md` — arquitetura e JSONs intermediários
- `docs/03-progresso.md` — log de trabalho
- `docs/04-resultados.md` — tempos medidos + estimativa 24 min

## Decisões de design (regras de ouro)

- Orquestração em Go; Python é só wrapper dos modelos ML.
- CPU only (`device_map="cpu"`, `dtype=float32`).
- Ferramentas gratuitas: OmniVoice, faster-whisper, demucs, ffmpeg, Google
  Translate endpoint gratuito.
- Respeita a fala original: não atropela nem acelera além de `--stretch-max`.
- Voz original como base: clone zero-shot por personagem da voz isolada.
- Naturalidade: pausas, isocronia, ducking, loudnorm -14 LUFS.
