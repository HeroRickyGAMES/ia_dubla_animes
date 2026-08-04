#!/usr/bin/env python3
"""
workers/tts.py — dublagem das falas com OmniVoice (CPU).

Reusa o MOTOR do ~/projetos/aiuto_trend_producer:
  modules/omnivoice_narrator.py  → pós-processamento broadcast + pausas
  modules/tts_narrator.py        → _strip_silence

Regras:
  * device_map="cpu", dtype=torch.float32 (nunca GPU)
  * clonagem zero-shot por personagem: ref = voz isolada do próprio anime
  * prompt de clone CACHEADO por personagem (create_voice_clone_prompt)
    → o ref não é re-encodado a cada fala (otimização de tempo)
  * não acelera a fala: speed padrão (1.0), sem atempo

Uso:
    .venv/bin/python workers/tts.py --lines work/lines.json --out work/dubs \
        --work work --engine ~/projetos/aiuto_trend_producer
"""
import argparse
import difflib
import json
import os
import re
import sys
import tempfile
import time

SAMPLE_RATE = 24000

# OmniVoice tem um artefato de "aquecimento" no início de quase toda fala:
# uma sílaba curta vocalizada (ouvida como "u/at/pat/vamos/ah") seguida de
# um silêncio antes da palavra real. Padrão típico: 1ª ilha sonora ≤ 0.6s +
# vão ≥ 0.25s até a próxima ilha. Removemos isso sem tocar na fala real.
ARTIFACT_MAX_FIRST = 0.6   # 1ª ilha acima disso não é artefato
ARTIFACT_MIN_GAP = 0.25    # vão mínimo antes da fala real
ARTIFACT_SHORT_FIRST = 0.25  # 1ª ilha bem curta + vão ≥ SHORT_GAP também é artefato
ARTIFACT_SHORT_GAP = 0.12
TAIL_MAX_LAST = 0.5        # cauda "resmungada" menor que isso é removida
TAIL_MIN_GAP = 0.4         # se vier depois de um silêncio desse tamanho

# O artefato "u/at" do OmniVoice costuma vir COLADO na fala real (gap < 0.25s),
# às vezes como um cluster de micro-ilhas. Regra adicional: mesclamos as ilhas
# iniciais com vãos < CLUSTER_GAP enquanto cada ilha for curta (≤ CLUSTER_ISLAND);
# se o cluster resultante for curto (≤ CLUSTER_MAX) e a próxima ilha for bem mais
# longa (≥ CLUSTER_RATIO), é aquecimento do modelo e cortamos.
ARTIFACT_CLUSTER_GAP = 0.06
ARTIFACT_CLUSTER_ISLAND = 0.25
ARTIFACT_CLUSTER_MAX = 0.35
ARTIFACT_CLUSTER_RATIO = 2.5


def _sound_islands(seg):
    """Ilhas sonoras (agrupadas por RMS) do AudioSegment, em segundos."""
    import numpy as np
    samples = np.frombuffer(seg.raw_data, dtype=np.int16).astype(np.float32) / 32768.0
    sr = seg.frame_rate
    ws = int(sr * 0.010)
    n = len(samples) // ws
    if n < 4:
        return []
    frames = samples[: n * ws].reshape(n, ws)
    rms = np.sqrt(np.mean(frames ** 2, axis=1))
    thr = max(rms.max() * 0.02, 0.0008)
    on = rms > thr
    out = []
    i = 0
    while i < n:
        if on[i]:
            j = i
            while j < n and on[j]:
                j += 1
            out.append((i * ws / sr, j * ws / sr))
            i = j
        else:
            i += 1
    return out


def _strip_leading_artifact(seg):
    """Remove o 'u/at' inicial (artefato do modelo) antes da fala real."""
    iso = _sound_islands(seg)
    if len(iso) < 2:
        return seg
    f0, f1 = iso[0]
    s0, _ = iso[1]
    first_len = f1 - f0
    gap = s0 - f1
    # caso clássico: 1ª ilha curta + vão claro até a fala real
    if first_len <= ARTIFACT_MAX_FIRST and gap >= ARTIFACT_MIN_GAP:
        cut_ms = int((s0 - 0.03) * 1000)  # mantém 30ms de respiro
        total_ms = len(seg)
        if 0 < cut_ms < total_ms * 0.5:
            return seg[cut_ms:]
    # caso curto: 1ª ilha bem curta + vão pequeno (artefato quase colado)
    if first_len <= ARTIFACT_SHORT_FIRST and gap >= ARTIFACT_SHORT_GAP:
        cut_ms = int((s0 - 0.03) * 1000)
        total_ms = len(seg)
        if 0 < cut_ms < total_ms * 0.5:
            return seg[cut_ms:]
    # caso colado: mescla micro-ilhas iniciais; se o cluster for curto e a
    # fala seguinte for bem mais longa, é aquecimento do modelo.
    cluster_end = f1
    for a, b in iso[1:]:
        if a - cluster_end >= ARTIFACT_CLUSTER_GAP:
            break
        if b - a > ARTIFACT_CLUSTER_ISLAND:
            break
        cluster_end = b
    cluster_len = cluster_end - f0
    next_len = 0.0
    for a, b in iso[1:]:
        if a >= cluster_end:
            next_len = b - a
            break
    if cluster_len <= ARTIFACT_CLUSTER_MAX and next_len >= cluster_len * ARTIFACT_CLUSTER_RATIO:
        cut_ms = int((cluster_end + 0.03) * 1000)
        total_ms = len(seg)
        if 0 < cut_ms < total_ms * 0.5:
            return seg[cut_ms:]
    return seg


def _strip_tail_junk(seg):
    """Remove uma cauda curta e isolada (resmungo final do modelo)."""
    iso = _sound_islands(seg)
    if len(iso) < 2:
        return seg
    p0, p1 = iso[-2]
    l0, l1 = iso[-1]
    if (l1 - l0) <= TAIL_MAX_LAST and (l0 - p1) >= TAIL_MIN_GAP:
        keep_ms = int(p1 * 1000) + 30
        total_ms = len(seg)
        if 0 < keep_ms < total_ms:
            return seg[:keep_ms]
    return seg


def _norm_word(w):
    """Normaliza palavra p/ comparação (minúsculas, só letras/dígitos)."""
    return re.sub(r"[^a-zà-ú0-9]", "", w.lower())


def _strip_leading_artifact_asr(seg, pt_text, whisper):
    """Strip guiado por ASR: remove conteúdo anterior à 1ª palavra que casa
    com o texto PT esperado.

    Resolve o artefato inicial ("at"/"Ed.") E o eco do ref_text (vazamento
    EN), porque nenhum dos dois casa com o PT. Preserva a 1ª palavra real
    (mesmo curta, ex. interjeição "É."/"Ah").

    Corte no END da última palavra não-casada (fim de palavra é timestamp
    confiável; início sofre lag do whisper e come conteúdo). Requer que a
    palavra não-casada seja substancial (>=4 chars) OU que haja silêncio
    real (>0.12s) entre ela e a 1ª palavra casada — assim nunca come o
    prefixo de uma palavra real que o whisper transcreveu mal.
    """
    if not pt_text or not whisper or not seg:
        return seg
    pt_words = [_norm_word(w) for w in pt_text.split()]
    pt_set = {w for w in pt_words if len(w) >= 2}
    # interjeições/artigos de 1 char reais do PT ("é", "a", "o") também casam
    pt_set |= {w for w in pt_words if len(w) == 1}
    if not pt_set:
        return seg
    tmp_path = None
    try:
        tmp_path = _seg_to_path(seg)
        segs, _ = whisper.transcribe(
            tmp_path, language="pt", word_timestamps=True)
        words = [(w.word, w.start, w.end) for s in segs for w in (s.words or [])]
    except Exception:
        return seg
    finally:
        if tmp_path:
            try:
                os.unlink(tmp_path)
            except OSError:
                pass
    if not words:
        return seg
    islands = _sound_islands(seg)

    def _island_of(t):
        for i, (a, b) in enumerate(islands):
            if a - 0.02 <= t <= b + 0.02:
                return i
        return -1

    for idx, (word, start, end) in enumerate(words):
        nw = _norm_word(word)
        if not nw:
            continue
        matched = nw in pt_set
        if not matched and len(nw) >= 4:
            best = max(
                (difflib.SequenceMatcher(None, nw, pw).ratio() for pw in pt_set),
                default=0.0)
            matched = best >= 0.7
        if not matched:
            continue
        if idx == 0:
            return seg
        pword, pstart, pend = words[idx - 1]
        pi = _island_of(pstart)
        mi = _island_of(start)
        # Artefato separado por silêncio (island distinta) OU por fronteira
        # de palavra do whisper (gap >= 0.12s — cobre respiro/eco colado que
        # o RMS junta numa island só). O gap guarda contra comer prefixo de
        # palavra real transcrita mal (ex. "É suposto" -> "fosse"): nesses
        # casos o whisper mantém as palavras contíguas (gap ~0).
        separated = (pi >= 0 and mi >= 0 and pi != mi) or (start - pend >= 0.12)
        if separated:
            cut_ms = int((pend + 0.03) * 1000)
            total_ms = len(seg)
            if 0 < cut_ms < total_ms * 0.6:
                return seg[cut_ms:]
        return seg
    return seg


def _seg_to_path(seg):
    """Salva um AudioSegment em arquivo temporário e devolve o caminho."""
    import tempfile
    tmp = tempfile.NamedTemporaryFile(suffix=".wav", delete=False)
    seg.export(tmp.name, format="wav")
    tmp.close()
    return tmp.name


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--lines", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--work", required=True)
    ap.add_argument("--engine", required=True,
                    help="pasta do aiuto_trend_producer (motor OmniVoice)")
    ap.add_argument("--max-lines", type=int, default=0, help="0 = todas")
    ap.add_argument("--speed", type=float, default=None,
                    help="fator de velocidade no OmniVoice (1.0 = natural; "
                         "default: isocronia via duration)")
    args = ap.parse_args()

    sys.path.insert(0, os.path.join(args.engine, "modules"))
    from omnivoice_narrator import OmniVoiceNarrator
    from tts_narrator import TTSNarrator

    import numpy as np
    import soundfile as sf
    import torch
    from pydub import AudioSegment
    from omnivoice import OmniVoice
    from faster_whisper import WhisperModel

    os.makedirs(args.out, exist_ok=True)
    engine_ov = OmniVoiceNarrator({})           # helpers (pos-process)
    engine_xtts = TTSNarrator({"tts": {}})      # helpers (_strip_silence)

    # ASR p/ strip guiado do artefato inicial + eco do ref_text (base, rápido).
    # Carrega do snapshot local do HF primeiro: robusto a rede instável.
    whisper = None
    try:
        import glob as _glob
        snap = sorted(_glob.glob(os.path.expanduser(
            "~/.cache/huggingface/hub/models--Systran--faster-whisper-base/"
            "snapshots/*")))
        if snap:
            whisper = WhisperModel(snap[0], device="cpu", compute_type="int8")
        else:
            whisper = WhisperModel("base", device="cpu", compute_type="int8")
        print("[tts] whisper base carregado (strip guiado por ASR)")
    except Exception as e:
        whisper = None
        print(f"[tts] sem whisper base ({e}); strip heurístico")

    with open(args.lines, encoding="utf-8") as f:
        data = json.load(f)
    lines = data["lines"]
    speakers = data.get("speakers", {})
    if args.max_lines > 0:
        lines = lines[:args.max_lines]

    print(f"[tts] carregando OmniVoice (cpu, float32)... "
          f"(primeira vez baixa o modelo)")
    model = OmniVoice.from_pretrained("k2-fsa/OmniVoice",
                                      device_map="cpu", dtype=torch.float32)

    emotions = {}
    emo_path = os.path.join(args.work, "emotion.json")
    if os.path.exists(emo_path):
        with open(emo_path, encoding="utf-8") as f:
            emotions = json.load(f)
    EMO_TAGS = {
        "happy": "[laughter] ",
        "sad": "[sigh] ",
        "angry": "[dissatisfaction-hnn] ",
        "surprised": "[surprise-ah] ",
    }

    prompts = {}
    for spk, info in speakers.items():
        ref_audio = info.get("ref_audio")
        ref_text = info.get("ref_text", "").strip()
        cache = os.path.join(args.work, "refs", spk + ".pt")
        if ref_audio and os.path.exists(ref_audio):
            try:
                if os.path.exists(cache):
                    from omnivoice import VoiceClonePrompt
                    prompts[spk] = VoiceClonePrompt.load(cache)
                    print(f"[tts] {spk}: prompt de clone do cache")
                else:
                    prompts[spk] = model.create_voice_clone_prompt(
                        ref_audio=ref_audio, ref_text=ref_text)
                    try:
                        prompts[spk].save(cache)
                    except Exception:
                        pass
                    print(f"[tts] {spk}: prompt de clone criado")
            except Exception as e:
                print(f"[tts] {spk}: sem prompt cacheado ({e}); "
                      "usarei ref_audio/ref_text por fala")
                prompts[spk] = None
        else:
            print(f"[tts] {spk}: SEM referência — voz padrão")
            prompts[spk] = None

    times = {}
    t_start = time.time()
    n_ok = n_err = 0
    for i, ln in enumerate(lines):
        line_id = ln["id"]
        spk = ln.get("speaker", "")
        text = (ln.get("pt") or "").strip()
        if not text:
            times[str(line_id)] = 0
            continue
        clean_text = text

        # isocronia: a fala dublada deve caber na janela da fala original.
        # O OmniVoice aceita `duration` (s) e preenche exatamente esse tempo,
        # distribuído entre as sentenças proporcionalmente ao texto.
        win = (ln.get("end") or 0) - (ln.get("start") or 0)
        target_dur = win * 0.95 if win > 0.3 else 0  # 95% deixa respiro

        t0 = time.time()
        out_path = os.path.join(args.out, f"{line_id:05d}.wav")
        last_err = None
        for attempt in range(3):
            try:
                emo = emotions.get(str(line_id), {})
                if emo.get("emotion") in EMO_TAGS and emo.get("conf", 0) >= 0.4:
                    text = EMO_TAGS[emo["emotion"]] + text
                # Otimização: texts curtos (≤220 chars) geram numa chamada
                # só — reutiliza o prefill do prompt de clone (~1.7x mais
                # rápido para falas multi-sentença). Só split se >220 chars
                # (safety do _dividir_em_sentencas).
                if len(text) <= 220:
                    kwargs = {"text": text, "language": "pt"}
                    if args.speed and args.speed > 0:
                        kwargs["speed"] = args.speed
                    elif target_dur > 0:
                        kwargs["duration"] = target_dur
                    p = prompts.get(spk)
                    if p is not None:
                        kwargs["voice_clone_prompt"] = p
                    else:
                        info = speakers.get(spk, {})
                        if info.get("ref_audio"):
                            kwargs["ref_audio"] = info["ref_audio"]
                            if info.get("ref_text"):
                                kwargs["ref_text"] = info["ref_text"]
                    audio = model.generate(**kwargs)
                    arr = np.asarray(audio[0], dtype=np.float32)
                    with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tmp:
                        sf.write(tmp.name, arr, SAMPLE_RATE)
                        seg = AudioSegment.from_wav(tmp.name)
                        os.unlink(tmp.name)
                    seg = engine_xtts._strip_silence(seg)
                    asr_stripped = _strip_leading_artifact_asr(seg, clean_text, whisper)
                    if asr_stripped is seg:
                        seg = _strip_leading_artifact(seg)
                    else:
                        seg = asr_stripped
                    seg = _strip_tail_junk(seg)
                    seg = seg.fade_in(8).fade_out(12)
                    final = seg
                else:
                    # Texto longo: split por sentença (fallback original)
                    sentences = engine_ov._dividir_em_sentencas(text)
                    total_len = sum(len(s) for s in sentences)
                    parts = []
                    for j, sent in enumerate(sentences):
                        kwargs = {"text": sent, "language": "pt"}
                        if args.speed and args.speed > 0:
                            kwargs["speed"] = args.speed
                        elif target_dur > 0 and total_len > 0:
                            kwargs["duration"] = target_dur * len(sent) / total_len
                        p = prompts.get(spk)
                        if p is not None:
                            kwargs["voice_clone_prompt"] = p
                        else:
                            info = speakers.get(spk, {})
                            if info.get("ref_audio"):
                                kwargs["ref_audio"] = info["ref_audio"]
                                if info.get("ref_text"):
                                    kwargs["ref_text"] = info["ref_text"]
                        audio = model.generate(**kwargs)
                        arr = np.asarray(audio[0], dtype=np.float32)
                        with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tmp:
                            sf.write(tmp.name, arr, SAMPLE_RATE)
                            seg = AudioSegment.from_wav(tmp.name)
                            os.unlink(tmp.name)
                        seg = engine_xtts._strip_silence(seg)
                        asr_stripped = _strip_leading_artifact_asr(seg, sent, whisper)
                        if asr_stripped is seg:
                            seg = _strip_leading_artifact(seg)
                        else:
                            seg = asr_stripped
                        seg = _strip_tail_junk(seg)
                        seg = seg.fade_in(8).fade_out(12)
                        parts.append((sent, seg))

                    final = AudioSegment.empty()
                    for j, (sent, seg) in enumerate(parts):
                        final += seg
                        if j < len(parts) - 1:
                            final += engine_ov._pausa_por_pontuacao(sent)

                final = engine_ov._pos_processar(final)
                final.export(out_path, format="wav")
                n_ok += 1
                break
            except Exception as e:
                last_err = e
                if attempt < 2:
                    print(f"[tts] fala {line_id} tentativa {attempt+1} falhou: {e}")
                    time.sleep(2)
        else:
            print(f"[tts] ERRO fala {line_id}: {last_err}")
            with open(os.path.join(args.work, "tts_errors.log"), "a") as ef:
                ef.write(f"{line_id}: {last_err}\n")
            n_err += 1
        times[str(line_id)] = round(time.time() - t0, 3)

        if (i + 1) % 25 == 0 or i == len(lines) - 1:
            el = time.time() - t_start
            print(f"[tts] {i+1}/{len(lines)} falas | "
                  f"{el:.0f}s | ~{el/max(i+1,1):.2f}s/fala")

    total = round(time.time() - t_start, 1)
    with open(os.path.join(args.work, "tts_times.json"), "w") as f:
        json.dump({"total_ms": int(total * 1000), "per_line_ms": times,
                   "n_ok": n_ok, "n_err": n_err}, f)
    print(json.dumps({"n": len(lines), "ok": n_ok, "err": n_err,
                      "total_seconds": total}))


if __name__ == "__main__":
    main()
