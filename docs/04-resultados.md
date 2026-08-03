# 04 — Resultados e estimativa (24 min)

Data de referência: 2026-08-02 (E2E completo: YameiiS01E01 EN→PT)

## Medições por estágio (máquina: 12 threads CPU, sem GPU)

Run completo: `./dub dub -i YameiiS01E01.mkv -o YameiiS01E01.dub.mkv --lang en`
(24 min, 341 falas, 5 personagens, 347 segmentos ASR + 18 splits de 2 vozes).

| Estágio | Tempo medido | Observação |
|---------|--------------|------------|
| probe + extract (ffmpeg) | ~0.1 s | cached entre runs |
| separação demucs (voz × fundo) | ~25 min (1ª vez) | cached depois; 24 min de áudio |
| ASR faster-whisper (en) | 141 s | small, int8, CPU |
| overlap SepFormer (2 vozes) | ~41 min | 128 falso-splits filtrados → 18 reais |
| diarização (ECAPA) + perfil de voz | ~33 s + refs 3.1 s | 5 clusters |
| tradução en→pt | ~17 s | 341 falas, workers paralelos |
| emoção por fala | ~10 s | cached |
| TTS OmniVoice (341 falas) | **13 450 s (~3.7 h)** | ~39 s/fala COM clone de voz |
| mix + mux (ffmpeg) | 959 s (~16 min) | ducking + loudnorm -14 LUFS |
| **TOTAL** | **14 429 s (~240 min)** | 4 h p/ 24 min de anime |

## Premissas usadas na estimativa

- Episódio de 24 min, ~350-400 falas, ~5-10 personagens falantes.
- CPU 12 threads, ffmpeg com `-preset veryfast`/`ultrafast` no vídeo.
- Modelos: faster-whisper `small`; OmniVoice float32; demucs `htdemucs` 2-stems.
- **TTS com clone de voz domina (~93% do tempo)**: ~39 s/fala. Sem clone
  (voz padrão) era ~18 s/fala, porém sem identidade de personagem.

## Resultado do teste (clipe real — E2E completo)

- Entrada: `YameiiS01E01.mkv` (1440 s, áudio EN)
- Saída: `YameiiS01E01.dub.mkv` (1.47 GB, 1440 s, h264+aac 96 kHz stereo)
- Nº de falas dubladas: **341/341** (TTS n_ok=341, n_err=0)
- Personagens: 5 (clusters ECAPA) — refs de voz por personagem gerados
  (`work/refs/spk_*.wav` + prompts de clone cacheados `spk_*.pt`)
- Overlap: 18 segmentos com 2 vozes simultâneas divididos em 36 falas
- Mix: loudnorm input_i -14.58 LUFS (target -14), ducking ativo
- Timeline: 233 falas `long` (aceleradas ≤15%), 36 `stretched`, 103 `clipped`
- Qualidade observada: sotaque do dublador EN (limitação cross-lingual do
  OmniVoice, documentada), melhora vs. run sem clone (voz por personagem).

## Lições

- O refs/ dir era apagado pelo stageOverlap e não recriado → TTS caía para
  voz padrão silenciosamente. Fix: recriar refs/dubs após invalidação.
- TTS pode falhar 1 fala em 341 por transiente de memória → retry (3x) +
  log de erros em `work/tts_errors.log` + pipeline falha se n_err>0.
- `stageOverlap` precisa ser idempotente: só reaplica split se o segmento
  original ainda existir no asr.json (evita duplicar ao retomar).

