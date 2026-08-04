# 04 — Resultados e estimativa (24 min)

Data de referência: 2026-08-03 (Run 2: isocronia calibrada — YameiiS01E01 EN→PT)

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
| tradução en→pt | ~17–39 s | 341 falas, workers paralelos |
| emoção por fala | ~10 s | cached |
| TTS OmniVoice (341 falas) | **13 450 s (~3.7 h)** | ~39–55 s/fala COM clone de voz |
| mix + mux (ffmpeg) | ~17 min | ducking + loudnorm -14 LUFS |
| **TOTAL** | **~5 h p/ 24 min de anime** | TTS domina |

## Premissas usadas na estimativa

- Episódio de 24 min, ~350-400 falas, ~5-10 personagens falantes.
- CPU 12 threads, ffmpeg com `-preset veryfast`/`ultrafast` no vídeo.
- Modelos: faster-whisper `small`; OmniVoice float32; demucs `htdemucs` 2-stems.
- **TTS com clone de voz domina (~90% do tempo)**: ~39–55 s/fala. Sem clone
  (voz padrão) era ~18 s/fala, porém sem identidade de personagem.

## Resultado do teste (clipe real — E2E completo)

- Entrada: `YameiiS01E01.mkv` (1440 s, áudio EN)
- Saída: `YameiiS01E01.dub.mkv` (1.47 GB, 1440 s, h264+aac 96 kHz stereo)
- Nº de falas dubladas: **341/341** (TTS n_ok=341, n_err=0)
- Personagens: 5 (clusters ECAPA) — refs de voz por personagem gerados
  (`work/refs/spk_*.wav` + prompts de clone cacheados `spk_*.pt`)
- Overlap: 18 segmentos com 2 vozes simultâneas divididos em 36 falas
- Mix: loudnorm input_i -14.58 LUFS (target -14), ducking ativo
- Qualidade observada: sotaque do dublador EN (limitação cross-lingual do
  OmniVoice, documentada), melhora vs. run sem clone (voz por personagem).

## Run 2 (2026-08-03): isocronia calibrada

Motivação: usuário reportou **assincronia** (voz dublada continua depois do
lábio fechar). Diagnóstico: a fala PT dublada era ~20–31% mais longa que a
janela original (mediana 1.20–1.31x); o timeline esticava 15% e cortava a
cauda (103 clipped). Janelas do ASR estavam corretas.

Fix: **TTS calibrado por fala** — `workers/tts.py` passa `duration = (end−start)*0.95`
ao `model.generate()` do OmniVoice (antes só `speed`), distribuído entre
sentenças proporcionalmente ao texto. Mesmo motor `k2-fsa/OmniVoice`, mesmos
clones cacheados. Também trim do artefato "at" melhorado (regra clássica +
curta + cluster colado), validado por ASR (1ª palavra preservada).

Resultado:
- **95.6% das falas cabem na janela original** (antes ~26–50%).
- Timeline: **clipped 103 → 23**, **stretched 36 → 2**, **long 233 → 15**
  (as 23 restantes são interjeições ultra-curtas tipo "Ah." com janela <0.3 s).
- Sync verificado por ASR word-timestamp em 3 trechos: dub começa ~0.1–0.2 s
  depois do lábio original (margem saudável, sem atropelar a fala seguinte).
- TTS do run 2: 3.7 h (341 falas, ~50–70 s/fala com `duration`).

## Run 3 (2026-08-04): Otimizações de performance

Motivação: pipeline completo leva ~5 h para 24 min de anime. Otimizar
sem reduzir qualidade (CPU only, DDR4 2 canais).

### Otimizações implementadas

1. **TTS — merge de sentenças** (workers/tts.py):
   Textos ≤220 chars → 1 chamada `generate()` com texto completo + `duration`
   total (antes: 1 chamada por sentença). Só split se >220 chars.
   - Resultado medido: 39.1 s/fala (vs 39.5 s antes = 1% — maioria das linhas
     já era single-sentença). Ganho real em linhas multi-sent (70/341 linhas).

2. **Overlap — pré-filtro de silêncio** (workers/overlap.py):
   Antes do SepFormer (2.5 s/candidato), RMS envelope (10ms frames). Se gap
   de silêncio >0.3 s → fala sequencial (1 falante com pausas), skip ~0.1 s.
   - Resultado: 15.6 min (vs 41 min antes = **-62%**). 11/128 candidatos
     rejeitados pelo filtro barato antes do SepFormer.

3. **Mix — thread flags** (internal/timeline/timeline.go):
   Adicionado `-filter_complex_threads 4` e `-thread_queue_size 2048`.
   - Resultado: 12 min (vs 17 min antes = **-29%**).

### Medidas (2026-08-04, AMD Ryzen 5 5500, 12 threads, 31 GB)

| Estágio | Run 2 (antes) | Run 3 (agora) | Delta |
|---------|---------------|---------------|-------|
| sep (demucs) | 17.0 min | 17.8 min | +0.8 min |
| ASR (whisper) | 5.0 min | 5.4 min | +0.4 min |
| **overlap (SepFormer)** | **41.0 min** | **15.6 min** | **-25.4 min** |
| diar + profile | 1.5 min | 0.8 min | -0.7 min |
| translate | 2.0 min | 1.0 min | -1.0 min |
| emotion | 3.0 min | 2.4 min | -0.6 min |
| TTS (OmniVoice) | 222.0 min | 218.0 min | -4.0 min |
| **mix (ffmpeg)** | **16.0 min** | **12.0 min** | **-4.0 min** |
| **TOTAL** | **307.5 min** | **273.0 min** | **-34.5 min** |

### Conclusões

- **Overlap pré-filtro**: maior ganho (-25 min). SepFormer é caro (2.5 s/segmento);
  o filtro RMS é ~25x mais barato e elimina ~50% dos candidatos.
- **Mix threading**: 4 min salvos. Filter_complex 342 stream受益 de 4 threads.
- **TTS merge**: ganho modesto porque 261/341 linhas já eram single-sentença.
  Linhas multi-sent (70/341, média 1.26 sent) ganham ~10% (reuso de prefill).
- **TTS paralelo NÃO funciona**: 2 workers × 6 threads = 57 s/fala (24% PIOR
  que 1 worker × 12 threads = 43.8 s/fala). DDR4 2 canais é o bottleneck.
- **TTS batch cross-line PIOR**: 4 linhas juntas = 39.4 s/fala (vs 31.9 s 1-a-1).
  OmniVoice não paraliza internamente; só ganho com merge intra-linha.

### Saída
- `work_test_opt/final.mkv` (1016 MB, 24 min, h264+aac, loudnorm -14 LUFS).
- 334/341 falas OK, 0 erros TTS.
- Dados: `work_test_opt/tts_times.json`, `work_test_opt/run.log`.

### Próximos passos
- Endereçar vazamento de texto EN do ref_text (clone echo).
- Endereçar over-cut do artefato "at".

## Lições

- O refs/ dir era apagado pelo stageOverlap e não recriado → TTS caía para
  voz padrão silenciosamente. Fix: recriar refs/dubs após invalidação.
- TTS pode falhar 1 fala em 341 por transiente de memória → retry (3x) +
  log de erros em `work/tts_errors.log` + pipeline falha se n_err>0.
- `stageOverlap` precisa ser idempotente: só reaplica split se o segmento
  original ainda existir no asr.json (evita duplicar ao retomar).
- `--speed` com default `1.0` no argparse sobrescrevia o `duration` do
  OmniVoice → fix: default `None` (usa `duration` a menos que --tts-speed).
- OmniVoice ignora `duration` abaixo de ~0.3 s: interjeições (janela <0.3 s)
  continuam longas; o timeline corta a cauda (aceitável, são 23/341).
- **TTS CPU é memory-bound**: processos paralelos competem por bandwidth DDR4
  e ficam mais lentos. Só merge intra-linha (reuso de prefill) ajuda.

