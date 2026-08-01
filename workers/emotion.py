#!/usr/bin/env python3
"""
workers/emotion.py — classifica a emoção de cada fala original (JA)
a partir do áudio: energia (RMS), F0 (mediana/desvio) e ritmo de fala.

Naturalidade de dublagem: "o personagem tá feliz? fica feliz, tá triste?
fica triste, tá bravo? fica bravo." Este estágio alimenta o TTS com a
emoção da fala ORIGINAL para a dublagem PT-BR atuar igual.

Saída: work/emotion.json  {"<line_id>": {"emotion": "...", "conf": 0.0}}
Emoções: neutral | happy | sad | angry | surprised

Classificação relativa ao baseline do próprio falante (mais robusto):
  * happy     → F0 acima do baseline + energia acima
  * sad       → energia abaixo + F0 abaixo + ritmo mais lento
  * angry     → energia acima + F0 no/abaixo do baseline + ritmo acima
  * surprised → F0 bem acima + desvio de F0 alto (salto de pitch)
  * neutral   → resto
"""
import argparse
import json

import librosa
import numpy as np
import soundfile as sf

_RATE_WORDS_PER_SEC = 1.0


def _line_features(y, sr):
    rms = float(np.sqrt(np.mean(y**2)) + 1e-12)
    energy_db = 20.0 * np.log10(rms)
    f0, voiced, prob = librosa.pyin(
        y, fmin=librosa.note_to_hz("C2"), fmax=librosa.note_to_hz("C6"), sr=sr
    )
    f0v = f0[~np.isnan(f0)]
    if len(f0v) < 10:
        f0m, f0s, voicing = 0.0, 0.0, 0.0
    else:
        f0m, f0s = float(np.median(f0v)), float(np.std(f0v))
        voicing = float(len(f0v)) / float(len(f0))
    return dict(energy_db=energy_db, f0m=f0m, f0s=f0s, voicing=voicing)


def _classify(feat, base):
    db, f0, f0s, voic = feat["energy_db"], feat["f0m"], feat["f0s"], feat["voicing"]
    bdb, bf0, bf0s = base["energy_db"], base["f0m"], base["f0s"]
    if f0 <= 0 or voic < 0.3:
        return "neutral", 0.0

    rel_f0 = (f0 - bf0) / (bf0 + 1e-9)
    rel_db = db - bdb
    high_energy = rel_db > 3.0
    high_f0 = rel_f0 > 0.2
    low_f0 = rel_f0 < -0.2
    high_var = f0s > bf0s * 1.5 + 20.0

    if high_energy and high_f0 and high_var:
        return "surprised", min(0.9, 0.5 + (f0 - bf0) / (bf0 * 2))
    if high_energy and not high_f0:
        return "angry", min(0.9, 0.5 + rel_db / 10.0)
    if low_f0 and not high_energy and db < bdb - 2.0:
        return "sad", min(0.8, 0.5 - rel_db / 8.0)
    if high_energy and high_f0:
        return "happy", min(0.8, 0.5 + rel_f0)
    return "neutral", 0.0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--audio", required=True)
    ap.add_argument("--segments", required=True, help="asr.json (com start/end/words)")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()

    y, sr = sf.read(args.audio, always_2d=False)
    if y.ndim > 1:
        y = y.mean(axis=1)
    with open(args.segments, encoding="utf-8") as f:
        asr = json.load(f)

    segs = asr.get("segments", [])
    feats = {}
    for s in segs:
        st, en = s.get("start", 0.0), s.get("end", 0.0)
        if en - st <= 0.1 or not s.get("text", "").strip():
            continue
        i0, i1 = int(st * sr), int(en * sr)
        if i1 > len(y) or i0 < 0 or i1 <= i0:
            continue
        feats[s["id"]] = _line_features(y[i0:i1], sr)

    # baseline por falante (cluster do diar.json quando disponível)
    import os
    diar_path = os.path.join(os.path.dirname(args.segments), "diar.json")
    assign = {}
    if os.path.exists(diar_path):
        with open(diar_path, encoding="utf-8") as f:
            d = json.load(f)
        assign = d.get("assign", {})

    speaker_feats = {}
    for sid, feat in feats.items():
        c = assign.get(sid, 0)
        speaker_feats.setdefault(c, []).append(feat)
    baselines = {}
    for c, fs in speaker_feats.items():
        baselines[c] = {
            "energy_db": float(np.median([f["energy_db"] for f in fs])),
            "f0m": float(np.median([f["f0m"] for f in fs])),
            "f0s": float(np.median([f["f0s"] for f in fs])),
        }

    out = {}
    for sid, feat in feats.items():
        c = assign.get(sid, 0)
        base = baselines.get(c, baselines.get(0, {"energy_db": -28, "f0m": 150, "f0s": 40}))
        emo, conf = _classify(feat, base)
        out[str(sid)] = {"emotion": emo, "conf": round(conf, 3)}

    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False)
    n_emo = sum(1 for v in out.values() if v["emotion"] != "neutral")
    print(json.dumps({"n": len(out), "emotive": n_emo}))


if __name__ == "__main__":
    main()
