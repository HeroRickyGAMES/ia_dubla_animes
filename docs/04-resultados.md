# 04 — Resultados e estimativa (24 min)

Data de referência: 2026-07-31

> A preencher com dados medidos do teste de ponta a ponta.

## Medições por estágio (máquina: 12 threads CPU, sem GPU)

| Estágio | Tempo medido | p/ episódio de 24 min (estimado) |
|---------|--------------|-----------------------------------|
| probe + extract (ffmpeg) | — | — |
| separação demucs (voz × fundo) | — | — |
| ASR faster-whisper (ja) | — | — |
| diarização + perfil de voz | — | — |
| tradução ja→pt | — | — |
| TTS OmniVoice (todas as falas) | — | — |
| mix + mux (ffmpeg) | — | — |
| **TOTAL** | — | — |

## Premissas usadas na estimativa

- Episódio de 24 min, ~350-400 falas, ~10 personagens falantes.
- CPU 12 threads, ffmpeg com `-preset veryfast`/`ultrafast` no vídeo.
- Modelos: faster-whisper `small`; OmniVoice float32; demucs `htdemucs` 2-stems.

## Resultado do teste (clipe real)

- Entrada: `...`
- Saída: `...`
- Qualidade observada / problemas:
- Nº de falas dubladas, Nº com conflito de voz×roteiro:
