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
import json
import os
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
TAIL_MAX_LAST = 0.5        # cauda "resmungada" menor que isso é removida
TAIL_MIN_GAP = 0.4         # se vier depois de um silêncio desse tamanho


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
    if (f1 - f0) <= ARTIFACT_MAX_FIRST and (s0 - f1) >= ARTIFACT_MIN_GAP:
        cut_ms = int((s0 - 0.03) * 1000)  # mantém 30ms de respiro
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


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--lines", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--work", required=True)
    ap.add_argument("--engine", required=True,
                    help="pasta do aiuto_trend_producer (motor OmniVoice)")
    ap.add_argument("--max-lines", type=int, default=0, help="0 = todas")
    ap.add_argument("--speed", type=float, default=1.0,
                    help="fator de velocidade no OmniVoice (1.0 = natural)")
    args = ap.parse_args()

    sys.path.insert(0, os.path.join(args.engine, "modules"))
    from omnivoice_narrator import OmniVoiceNarrator
    from tts_narrator import TTSNarrator

    import numpy as np
    import soundfile as sf
    import torch
    from pydub import AudioSegment
    from omnivoice import OmniVoice

    os.makedirs(args.out, exist_ok=True)
    engine_ov = OmniVoiceNarrator({})           # helpers (pos-process)
    engine_xtts = TTSNarrator({"tts": {}})      # helpers (_strip_silence)

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

        t0 = time.time()
        out_path = os.path.join(args.out, f"{line_id:05d}.wav")
        last_err = None
        for attempt in range(3):
            try:
                emo = emotions.get(str(line_id), {})
                if emo.get("emotion") in EMO_TAGS and emo.get("conf", 0) >= 0.4:
                    text = EMO_TAGS[emo["emotion"]] + text
                sentences = engine_ov._dividir_em_sentencas(text)
                parts = []
                for sent in sentences:
                    kwargs = {"text": sent, "language": "pt",
                              "speed": args.speed if args.speed and args.speed > 0 else None}
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
                    seg = engine_xtts._strip_silence(seg)
                    seg = _strip_leading_artifact(seg)
                    seg = _strip_tail_junk(seg)
                    seg = seg.fade_in(8).fade_out(12)
                    parts.append((sent, seg))
                    os.unlink(tmp.name)

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
