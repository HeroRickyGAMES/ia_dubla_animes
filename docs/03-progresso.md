# 03 — Log de progresso (work log)

Mantenha este arquivo atualizado a cada etapa. Outras IAs consultam aqui
para saber o que já foi feito e o que falta.

## 2026-07-31 — Construção inicial + primeiro E2E (parcial)

### Feito
- [x] Pesquisa: OmniVoice (motor de dublagem) + naturalidade (docs/01-pesquisa.md)
- [x] Arquitetura do pipeline definida (docs/02-arquitetura.md)
- [x] Go 1.26.5 instalado em `/home/heroricky/projetos/go/bin/go` (NOTA: o
      diretório `go1.26.5` NÃO existe mais; usar `go`, não `go1.26.5`)
- [x] venv criado (`.venv`, Python 3.12.3); dependências instaladas:
      torch 2.13.0+cpu, torchaudio 2.11.0+cpu, faster-whisper 1.2.1,
      omnivoice 0.2.1, demucs 4.1.0, librosa 0.11.0, scikit-learn 1.9.0,
      speechbrain 1.1.0 (instalado à parte, entrou no requirements depois
      do pip inicial)
- [x] Workers Python: asr.py, sep.py, diar.py, tts.py
- [x] Orquestrador Go: dub.go + internal/ (cli, pipeline, ffmpeg, srt,
      translate, voice, timeline, report) — `go build -o dub .` e
      `go vet ./...` OK
- [x] `./dub check` OK (ffmpeg, ffprobe, venv, engine aiuto_trend_producer)
- [x] APIs verificadas contra o pacote instalado:
      `omnivoice.OmniVoice.from_pretrained("k2-fsa/OmniVoice", device_map="cpu",
      dtype=torch.float32)`, `create_voice_clone_prompt(ref_audio=, ref_text=)`,
      `VoiceClonePrompt.save(path)` / `load(path, map_location="cpu")`,
      `model.generate(text=, voice_clone_prompt=)` → list[np.ndarray] @ 24kHz.
      Helpers do engine (`_dividir_em_sentencas`, `_pausa_por_pontuacao`,
      `_pos_processar`, `_strip_silence`) confirmados em
      modules/omnivoice_narrator.py e modules/tts_narrator.py.
- [x] Clipe de teste JA criado: `testdata/ep_test.mp4` (67 s) montado a
      partir de 12 clipes do Mozilla Common Voice (CC0, split train ja,
      dataset `fixie-ai/common_voice_17_0` via HF datasets-server),
      3 falantes (1♀ + 2♂), cada clipe separado por 0.5 s de silêncio.
      Script do fetch em `/tmp/opencode/fetch_cv.py` (perdido no reboot —
      refazer se precisar).
- [x] Primeiro E2E: `./dub dub -i testdata/ep_test.mp4 -o out/ep_test.mkv
      --model small --max-lines 8`
      - ASR (faster-whisper small): 11 falas com word-timestamps ✓
      - diarização: 3 clusters detectados (MFCC fallback; ECAPA quebrado
        com torch 2.13) ✓
      - perfil acústico por personagem ✓ (valores a conferir no report)
      - tradução ja→pt ✓ após fix do endpoint gtx (era multi-&q=, quebrado;
        agora 1 request/fala com workers paralelos)
      - TTS OmniVoice: modelo 2.4 GB sendo baixado (1ª execução) — NÃO
        concluído ainda (máquina reiniciou no meio da 1ª tentativa)
      - mix+mux: ainda não executado neste run
- [x] Flag `--lang CODE` adicionada (default `ja`; dubla para pt). Útil
      p/ testar com o anime em inglês (Makeine) no teste final.
- [x] Clipe de teste real do usuário: `[Yameii] Makeine - Too Many Losing
      Heroines! - S01E01 [English Dub] [CR WEB-DL 1080p] [D526979D].mkv`
      (1.46 GB, na raiz do projeto). NOTA: áudio é ENGLISH (1 track),
      sem trilha japonesa → teste final com `--lang en`.

### Bugs corrigidos (nesta sessão)
1. `internal/ffmpeg/ffmpeg.go` — lista de concat com paths relativos era
   resolvida contra o diretório da lista → `work/refs/work/refs/...`.
   Fix: paths absolutos no arquivo .list.
2. `workers/sep.py` — `AudioFile.read()` do demucs devolve SÓ o wav (não
   (wav, sr)); o unpack quebrava. Fix: `wav = read(...)`, trata mono 1-D,
   usa `model.samplerate`.
3. `workers/sep.py` (2ª rodada) — quebrava no forward do htdemucs:
   `shape '[1, 4, -1, 343980]' is invalid for input of size 2822400`
   (erro interno demucs com segment=8). FIX: default `--segment 4.0` —
   demucs separou 67 s em ~57 s com segment 4 (testado em 19:56, vocals.wav
   e nv.wav gerados).
4. `workers/diar.py` — `sf.read(always_2d=True)` mantinha (N,1) e MFCC
   recebia sinais 2-D → shapes inconsistentes. Fix: colapsar para mono
   sempre. Também: ECAPA falha em runtime com torch 2.13 (F.pad 4D) →
   fallback MFCC automático implementado.
5. `internal/pipeline/run.go` — conflito fantasma sem roteiro: `Expected`
   zero-value era `""` (≠ `RoleUnknown`). Fix: skip quando `""`.

### Notas
- NAS `/home/heroricky/NAS/share/...` é storage de rede: operações de
  arquivo (tar, builds, downloads de modelo) ficam lentas. Timeouts generosos.
- `go` build: `/home/heroricky/projetos/go/bin/go`.
- Sem sudo no ambiente.
- Modelos no cache HF do usuário (~/.cache/huggingface):
  Systran/faster-whisper-small (464 MB), adefossez/HTDemucs (81 MB),
  speechbrain/spkrec-ecapa-voxceleb (85 MB), k2-fsa/OmniVoice (2.4 GB).
- Rodar o pipeline com `setsid nohup ./dub ... > run.log 2>&1 &` — a tool
  de shell mata jobs em background ao expirar; setsid desgruda do grupo.
- ECAPA (speechbrain) está quebrado com torch 2.13 (NotImplementedError
  em F.pad para 4D). O fallback MFCC funciona. Se quiser ECAPA, testar
  speechbrain>=1.1.0 com torch 2.12 ou remendar o CNN pad.

### Pendências
- [x] TTS OmniVoice concluído (dubs gerados); rever com `language="pt"` no
      run 20:44 (em andamento)
- [x] sep (demucs) OK com `--segment 4.0` (testado 67 s em ~57 s)
- [x] mix+mux: filtro corrigido (asplit); pendente run completo terminar
- [ ] Relatório com tempos reais por estágio + estimativa 24 min
      (docs/04-resultados.md)
- [ ] Validar perfil de voz (F0/HNR/centroid → sexo/idade) contra os
      3 falantes conhecidos do clipe
- [ ] Conferir emoção em dados com atuação real (anime); calibrar limiares
- [ ] Teste final com o Makeine EN (`--lang en`): ref_text EN mais limpo,
      sep real no áudio do anime

## 2026-07-31 — Run 2: TTS concluído, mix corrigido, estágio de emoção

### TTS (Run 1 → Run 2)
- Download do OmniVoice (2.4 GB, ~50 min throttled sem HF_TOKEN) concluído
  ~20:20. TTS gerou as 8 falas (20:21–20:26, ~1 dub/2 min).
- Preview aprovado como "ok", porém: (a) sotaque JA no PT-BR, (b) intonação
  monótona ("sem a intonação ótima").
- DIAGNÓSTICO (docs oficiais do OmniVoice):
  - README: "In cross-lingual voice cloning ... the generated speech will
    carry an accent from the reference audio's language." → o sotaque JA
    em PT-BR é limitação DOCUMENTADA do modelo quando o ref é voz JA.
    Mitigação: `language="pt"` em `model.generate()` (doc: "Performance is
    slightly better if you specify the language"). Issue #127 (EN→PT-BR
    dubbing) foi fechada sem fix no modelo.
  - Intonação monótona = greedy decoding: `class_temperature=0.0` (default).
    Parâmetros de sampling documentados em docs/generation-parameters.md.
  - Emoção ("dublador de verdade"): NÃO existe vocabulário de emoção no
    `instruct` (só gender/age/pitch/whisper/accent). Voice Design é treinado
    só EN/ZH ("may produce unstable results"). O caminho documentado são as
    TAGS INLINE não-verbais: `[laughter]`, `[sigh]`, `[confirmation-en]`,
    `[question-en/ah/oh/ei/yi]`, `[surprise-ah/oh/wa/yo]`,
    `[dissatisfaction-hnn]` — tokenizadas standalone (funcionam em qualquer
    idioma).
- DECISÃO DO USUÁRIO: manter o ESCOPO original (voz por personagem, clone
  da voz isolada do próprio anime). NÃO usar a voz de referência PT-BR do
  engine base (`assets/voices/minha_voz_ref.wav`).

### Bugs corrigidos (run 2)
6. `internal/timeline/timeline.go` — mix quebrava no ffmpeg 6.1.1 com
   `Error initializing complex filters` / `Stream specifier 'dial' matches
   no streams`: o label `[dial]` alimentava DOIS filtros
   (`sidechaincompress` + amix final) e o ffmpeg 6.1.1 rejeita reuso de
   label com sidechain. Fix: `[dial]asplit=2[dsc][dmix]` — um braço para o
   sidechain, outro para o mix final. Testado OK manualmente (67 s mkv,
   speed ~12x).

### Novo estágio: emoção por fala (naturalidade de dublagem)
- `workers/emotion.py`: por fala original calcula RMS (dB), F0 mediana/
  desvio (librosa.pyin) e classifica relativo ao baseline do FALANTE:
  happy/sad/angry/surprised/neutral → `work/emotion.json`
  `{line_id: {emotion, conf}}`. (Atual: energia alta+F0 acima = happy;
  energia baixa+F0 abaixo = sad; energia alta+F0 não acima = angry;
  F0 alto+desvio alto = surprised.)
- `workers/tts.py`: lê emotion.json; se conf ≥ 0.4, prefixa a fala com a
  tag de atuação (happy→`[laughter]`, sad→`[sigh]`, angry→
  `[dissatisfaction-hnn]`, surprised→`[surprise-ah]`). Sempre passa
  `language="pt"` no `generate`.
- Validação: clipe de teste é leitura neutra do Common Voice → tudo
  `neutral` (correto). Testar com anime real (atuação emocional).
- Rodada nova em 20:44 (mesmo comando): emotion 10 s, TTS regenerando 8
  falas com tts.py novo. mix/mux agora com asplit.

### Notas (run 2)
- f0/energia/ritmo por fala do clipe: spk♀ f0≈197–228 Hz, spk♂ f0≈111–144 Hz
  (3 clusters batem com os 3 falantes conhecidos).
- `ffmpeg -h filter=amix` confirma `normalize` e `dropout_transition` OK no
  6.1.1; o problema era exclusivamente o reuso de label.


### Run 3 (21:19–22:18): Makeine EN→PT em fatia de 3 min
- Alvo real: `[Yameii] Makeine - Too Many Losing Heroines! - S01E01`, fatia
  960–1140 s (`testdata/makeine_slice.mp4`), `--lang en --max-lines 20`,
  `--work work_makeine_slice` → `out/makeine_slice.mkv` (180 s, 20 falas,
  5 personagens, 22.1 min de processamento, ~62 s/fala de TTS).
- **BUG (crítico): tradução caía inteira para inglês.** Em
  `internal/translate/translate.go` + `internal/pipeline/run.go`, se UM
  lote do Google falhasse, `err != nil` → `pts = texts` (TODAS as falas
  voltavam ao original). Primeiro run gerou dubs em inglês.
  Fix: fallback **por fala** — `translateBatch` devolve resultados
  parciais, worker só grava `""` nas falhas, pipeline troca só as vazias
  pelo original e loga `N/58 falhas mantidas no original`.
- Retry adicionado no `translateOne` (2 tentativas, backoff 700 ms) após
  timeouts do endpoint gratuito. No run 2 de translate, 3/58 falharam
  (só a fala 0 entre as 20 do TTS ficou em inglês).
- **Observação do usuário**: dublagem "engraçada" — sotaque de inglês
  tentando falar português, mas qualidade boa. É a limitação conhecida do
  clone cross-lingual do OmniVoice (carrega sotaque do ref; ref = dublador
  EN do anime). Aceito como característica por enquanto.
- Mix final: 180.022 s, input_i -15.0 LUFS (target -14).
- est. 24 min: 177 min (~3 h) na fatia EN (TTS domina: 1242 s / 20 falas).
- Feedback do usuário (Run 3):
  - Tradução: ótima.
  - Sotaque EN aparece principalmente na **voz masculina** (ref do dublador
    EN masculino); notar para eventual melhoria de naturalidade.
  - "Dublado pela metade": esperado do teste — `--max-lines 20` dublou só
    20 das 58 falas da fatia (além de 9 `long`/9 `shifted` no timeline).
    Para run completo, subir `--max-lines` ou rodar a fatia inteira.

## 2026-08-02 — Run completo YameiiS01E01 EN→PT (E2E 100%)

### Descobertas
- **O episódio é inglês** (auto-detect en 0.88–0.99 em várias amostras);
  `--lang ja` força ALUCINAÇÃO de kanji/kana (213/230 segs com kana).
  Rodar SEMPRE com `--lang en` para este arquivo. A dublagem é EN→PT.
- `work/vocals.wav` (demucs) sai em **44.1 kHz**, não 16k: SepFormer e
  slices assumindo 16k precisam de resample (librosa → target_sr=16000).

### Overlap (2 vozes simultâneas) — sep.py e overlap.py
- **BUG do shape**: `separate_batch` retorna `(1, samples, 2)`; o unpack
  criava 184k stems. Fix em `workers/overlap.py`: `est = est[0]` e, se
  `(samples, 2)`, transpor → `(2, samples)`.
- **128 falso-splits**: sem filtro, o SepFormer dividia até fala única
  (stem2 = eco/resíduo re-transcrito). Fix: `--min-energy-ratio 0.25`
  (voz fraca ≥25% da energia da forte) + `--max-sim 0.5` (difflib
  SequenceMatcher entre os textos). Resultado: **18 splits reais** (36
  falas; ex. seg 58 e 96 viram 2 falas com textos distintos de fato).
- `stageOverlap` (Go) NÃO aplicava os splits quando `overlap.json` já
  existia (cache). Fix: aplicar sempre, e torná-lo **idempotente** (só
  reaplica se o segmento original ainda existir no asr.json).

### Diarização ECAPA
- `workers/diar.py` gravava `"method":"mfcc"` mesmo com ECAPA (label fixo).
  Fix: `"ecapa" se use_ecapa senão "mfcc"`. ECAPA validado standalone
  (192-dim, norms 264–355); 5 clusters no E2E.

### Bugs corrigidos
- **refs vazios (crítico)**: `stageOverlap` fazia `RemoveAll(work/refs)` e
  `RemoveAll(work/dubs)` ao invalidar estágios derivados, mas os diretórios
  só eram criados no início do `Run()` → `refs/` não existia quando
  `stageSpeakers` slicava → TODOS os speakers "sem trechos de voz" → TTS
  usava voz padrão (sem clone) silenciosamente. Fix: recriar `refs/` e
  `dubs/` logo após a remoção (run.go).
- **TTS fala 46 falhou 1x em 341** (transiente, funciona isolada) sem ser
  visível: o stdout do worker só era logado com `-v`. Fix em `workers/tts.py`:
  retry 3x por fala + erros gravados em `work/tts_errors.log`; e
  `stageTTS` agora falha se `tts_times.json` reportar `n_err > 0` (não
  deixa o mix rodar com dub faltando).

### E2E completo (17:08–21:08, ~4 h)
- `./dub dub -i YameiiS01E01.mkv -o YameiiS01E01.dub.mkv --lang en`
- 347 segmentos ASR (329 + 18 splits×2 − originais), 341 falas dubladas,
  5 personagens com clone de voz (refs + prompts .pt cacheados).
- TTS: 341/341 OK (~3.7 h, ~39 s/fala com clone); mix 16 min; total ~4 h.
- Saída: `YameiiS01E01.dub.mkv` (1.47 GB, 1440 s, h264 + aac 96 kHz,
  loudnorm -14 LUFS). Dados em docs/04-resultados.md.
- Flags timeline: 233 long / 36 stretched / 103 clipped (ver timeline.go).

## 2026-08-03 — Isocronia: TTS calibrado por fala (duration) + trim do artefato

### Contexto
- Usuário assistiu `YameiiS01E01.dub.mkv` no celular e reportou **assincronia**
  (a voz dublada continua depois do lábio fechar).

### Diagnóstico (análise acústica + timeline)
- Causa raiz NÃO era só o artefato "at": a fala PT dublada é **~20–31% mais
  longa que a janela original** (ratio: mediana 1.20–1.31, p75 1.62–1.77,
  p90 2.14–2.40). Só ~26–50% dos dubs cabiam na janela.
- O timeline estica no máx. 15% e depois **corta a cauda** (`clipped` = 103
  falas) → a voz continua depois do lábio fechar. Janelas do ASR originais
  estão corretas; o problema é o TTS PT ser mais longo que o original EN.
- Artefato "at": análise de espectro mostrou 1ª ilha curta (mediana 0.19 s,
  até 0.28 s), gap mediano 0.05 s (colado), centroid ~5 kHz (vs ~2 kHz da
  fala real), RMS ratio ~1.0.

### Fix (escolhido pelo usuário)
- **2-pass TTS com calibração por fala** usando o parâmetro **`duration`** do
  OmniVoice (sobrescreve `speed`; validado empiricamente: line 0 natural
  6.38 s → duration=4.1 → 4.28 s; line 2 3.44 → 3.12 s).
- Mesmo motor `k2-fsa/OmniVoice` (CPU), mesmos clones cacheados
  (`work/refs/spk_*.pt`) — só muda o parâmetro de duração por fala.
- `workers/tts.py`:
  - Passa `duration = (end−start) * 0.95` ao `generate()`, distribuído entre
    sentenças proporcionalmente ao texto (multi-sentença: 70/341 linhas,
    média 1.26 sentenças).
  - `--speed` agora default `None` (antes 1.0 fazia o `duration` nunca
    ser usado — bug do branch).
  - Trim do artefato melhorado: regra clássica (1ª ilha ≤0.6 s + gap ≥0.25 s)
    + regra curta (≤0.25 s + gap ≥0.12 s) + **regra de cluster colado**
    (micro-ilhas com vão <0.06 s, cluster ≤0.35 s e fala seguinte ≥2.5x).
- Validação: 3 falas-teste caem de excesso +1.45/+2.79 s para −0.25/−0.30/−0.21 s
  (cabem na janela). ASR confirma que o trim NÃO corta a 1ª palavra real.

### Run de re-dublagem (em andamento)
- Regerando 341 dubs com o novo tts.py (~5–6 h, ~50–70 s/fala com clone).
- Depois: mix (timeline.go já OK; stretch/clip vira caso raro) + QA.
- Est. ~6.6 h de TTS para 341 falas; validar isocronia no final e commit+push.

## 2026-08-04 — Otimizações de performance (sem perder qualidade)

### Diagnóstico (medidas em CPU AMD Ryzen 5 5500, 12 threads, 31 GB RAM)
- **TTS paralelo NÃO funciona** — memória bound (DDR4 2 canais):
  - 1 worker × 12 threads: 43.8 s/fala, ~6x CPU efetivo
  - 2 workers × 6 threads: 57 s/fala (24% PIOR por contenção de bandwidth)
  - 3 workers × 4 threads: 128 s/fala (3x PIOR)
  - **Conclusão: processos paralelos competem por bandwidth DDR4, não ajudam**
- **Batch de linhas diferentes = PIOR** (39.4 s/fala batch4 vs 31.9 s/fala 1-a-1)
- **RAM do OmniVoice: 2.55 GB pico por worker** (3.5 GB em disco)
- **CPU utilization**: worker único usa ~6 de 12 threads efetivamente (matmul
  limitado por bandwidth, não por cores)

### Otimização 1: TTS — merge de sentenças em 1 chamada generate()
- **Problema**: 341 falas = 430 chamadas `generate()` (sentenças separadas).
  Cada chamada re-processa o prompt de clone (prefill).
- **Fix**: textos ≤220 chars → 1 chamada com o texto completo + duration total.
  Splits em sentenças só se >220 chars (fallback).
- **Resultado**: 6 linhas multi-sentença: 39.2 s/fala (vs 43.8 s antes = 10% mais
  rápido). Estimativa total: ~16% economia no TTS (3.7 h → ~3.1 h).
- **Qualidade**: WAVs válidos (24kHz, 1.3–6.5s); o modelo OmniVoice lida com
  pontuação interna; pausas preservadas pelo próprio modelo.

### Otimização 2: overlap — pré-filtro de silêncio interno
- **Problema**: SepFormer roda em TODOS os 128 candidatos (≥2.5 s) → só 18 splits
  reais. 41 min desperdiçados em fala única.
- **Fix**: antes do SepFormer, checar RMS envelope (10ms frames). Se há gap de
  silêncio >0.3 s → é fala sequencial (pausas de 1 falante), não sobreposição.
  Skip barato (~0.1 s por candidato) antes do SepFormer caro.
- **Resultado estimado**: ~50% dos candidatos têm pausas internas → ~20 min economizados.
- **Qualidade**: sem perda (só pulamos candidatos que NÃO são sobreposição real).

### Otimização 3: mix — thread flags do ffmpeg
- **Problema**: 341 inputs no filtergraph + amix 342 streams → 17 min.
  Filtergraph do ffmpeg roda single-threaded por padrão.
- **Fix**: adicionado `-filter_complex_threads 4` e `-thread_queue_size 2048`.
- **Resultado**: estimativa ~30–50% redução (17 min → 9–12 min).
- **Qualidade**: sem perda (só paralelismo interno do ffmpeg).

### Notas de hardware
- Ryzen 5 5500 = 6 cores / 12 threads, 2 canais DDR4 → bandwidth limita TTS.
- TTS com clone zero-shot é memory-bound (pesos 3.5 GB float32, autoregressive).
- Paralelizar TTS em múltiplos processos piora por competição de bandwidth.
- Otimizações que ajudam: reduzir chamadas (merge), pré-filtrar (overlap),
  paralelizar ffmpeg filtergraph (mix).

## 2026-08-04 — Fix de qualidade: vazamento EN do ref_text + artefato "at" (strip guiado por ASR)

### Diagnóstico final do vazamento EN
- **Mecanismo**: `OmniVoice._combine_text` concatena `ref_text.strip() + " " + text`
  (omnivoice.py L1701) — conteúdo EN do ref é tokenizado e **ecoado** no áudio PT.
- **Testes empíricos (1–5)**: ref_text cheio (A) é o que preserva melhor a voz;
  neutro "Hmm." (B) degrada (garble, perda de conteúdo); ref PT traduzido (D)
  ecoa o conteúdo do ref; vazio (C) é instável. **Ref_text cheio fica**.
- **Leak é estocástico**: line 9 vazou "É certo!" na produção mas saiu limpa em
  regenerações com os mesmos inputs. `class_temperature` sem efeito consistente.
- **Centroid espectral e gate por idioma não discriminam**: eco curto passa como
  pt pelo whisper; artefato tem centroid similar a fala real.
- **Amostra 40 linhas da produção**: 13/40 faltam a 1ª palavra PT (over-cut
  heurístico real, ex. "entendi"→"Temgi"); leaks EN reais: line 31 ("Say no!"),
  line 46 ("freak out!"). Artefato "at" em line 25 ("Ed.") e line 60 ("Aia! Aia!"
  sub-cut, não removido).

### Fix implementado: strip guiado por ASR em workers/tts.py
- `_strip_leading_artifact_asr(seg, pt_text, whisper)`: transcreve o dub com
  faster-whisper base (int8, `word_timestamps=True`) e remove tudo antes da
  1ª palavra que **casa com o texto PT esperado** (match exato ou fuzzy ≥0.7
  p/ palavras ≥4 chars).
- **Corte no END da última palavra não-casada** (fim de palavra é timestamp
  confiável; início sofre lag do whisper e come conteúdo).
- **Guarda anti over-cut**: só corta se a palavra não-casada estiver separada
  por silêncio (island RMS distinta) OU gap de palavra do whisper ≥0.12s —
  cobre respiro/eco colado, mas NÃO come prefixo de palavra real transcrita
  mal (ex. "É suposto"→"fosse" tem gap 0.0 → não corta).
- Interjeições reais de 1 char do PT ("é", "a", "o", "ah") também casam →
  preservadas.
- Integrado nos dois paths do loop (≤220 e >220); heurística RMS antiga
  (`_strip_leading_artifact`) vira fallback quando o ASR não corta.
- Whisper carrega do snapshot local do HF (`~/.cache/.../faster-whisper-base`)
  — robusto a rede instável; fallback gracioso para heurística se falhar.
- Custo: ~1.25 s/fala (+~7 min em 341 falas ≈ +3% do TTS). Limpeza do tmp
  file do path ≤220 (vazava) incluída.

### Validação (produção + regenerações)
- l9 (leak "É certo!"): **corta 3.55 s, remove o eco EN inteiro, PT íntegro**:
  "Pense nisso, você fez o seu melhor irmão mais velho, deve ter sido difícil,
  eu sei." ✓
- l25 (artefato "Ed."): corta 0.31 s, "Ela" intacta ✓
- l160/l289 (artefatos "Ai,"/"E,"): cortam 0.27/0.23 s, conteúdo íntegro ✓
- l48 ("É suposto" transcrito "fosse"): **NÃO corta** (anti over-cut) ✓
- Linhas limpas (00025, l60, l141): 0.00 s cortados ✓
- Amostra aleatória 25 linhas: 4 cortadas, **0 over-cut** (1ª palavra PT
  sempre presente no resultado) ✓
- `./dub check` + `go build -o dub .` + `go vet ./...` + `py_compile` OK.
- Próximo passo: rodar TTS parcial (`--force` num subset) para validar a
  integração no fluxo real; depois commit+push e (opcional) re-dublagem
  completa do ep01 para QA final.
