#!/usr/bin/env python3
"""
workers/overlap.py — separa falas com DUAS vozes ao mesmo tempo (SepFormer).

Problema: quando 2 personagens falam juntos, o faster-whisper junta tudo num
segmento só e a diarização atribui a mistura a um único cluster → a fala sai
dublada com a voz errada ou mesclada.

Solução (Apache-2.0):
  1. Candidatos: segmentos longos (--min-dur, default 2.5s) → provável mix.
  2. SepFormer (speechbrain/sepformer-wsj02mix, wsj0-2mix) separa 2 vozes.
  3. Re-ASR (faster-whisper) em cada voz separada para achar o texto e o
     intervalo exato de cada uma.
  4. Se as DUAS vozes têm fala real → vira 2 falas (split). Se só uma tem,
     o segmento era normal e é mantido.

Saída: work/overlap.json com lista de splits. O orquestrador Go substitui
o segmento original pelas partes (novos ids únicos) em asr.json e o resto
do pipeline (diar, perfil, tradução, TTS) trata cada voz separadamente.

Uso:
    .venv/bin/python workers/overlap.py --audio work/vocals.wav \
        --segments work/asr.json --out work/overlap.json --model small
"""
import argparse
import json
import time


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--audio", required=True)
    ap.add_argument("--segments", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--model", default="small")
    ap.add_argument("--lang", default="ja")
    ap.add_argument("--min-dur", type=float, default=2.5)
    ap.add_argument("--min-part", type=float, default=0.35,
                    help="fala mínima de uma voz separada p/ virar linha")
    ap.add_argument("--min-energy-ratio", type=float, default=0.25,
                    help="voz mais fraca deve ter ≥ X da energia da mais forte "
                         "(SepFormer separa até fala única → stem2 vira eco/resíduo)")
    ap.add_argument("--max-sim", type=float, default=0.5,
                    help="similaridade de texto máx. entre as 2 vozes p/ virar split "
                         "(fala única → mesmo texto nos 2 stems)")
    args = ap.parse_args()

    import numpy as np
    import soundfile as sf

    t0 = time.time()
    with open(args.segments, encoding="utf-8") as f:
        segs = json.load(f)["segments"]

    data, sr = sf.read(args.audio, dtype="float32", always_2d=True)
    data = data.mean(axis=1)
    if sr != 16000:
        import librosa
        data = librosa.resample(data, orig_sr=sr, target_sr=16000)
        sr = 16000

    cand = [s for s in segs if (s["end"] - s["start"]) >= args.min_dur]
    if not cand:
        with open(args.out, "w", encoding="utf-8") as f:
            json.dump({"splits": []}, f)
        print(json.dumps({"candidates": 0, "splits": 0,
                          "seconds": round(time.time() - t0, 1)}))
        return

    print(f"[overlap] {len(cand)} candidatos (≥ {args.min_dur:.1f}s) "
          f"— carregando SepFormer (cpu)...")
    import torch
    try:
        from speechbrain.inference.separation import SepformerSeparation
    except ImportError:
        from speechbrain.pretrained import SepformerSeparation
    sep = SepformerSeparation.from_hparams(
        source="speechbrain/sepformer-wsj02mix",
        run_opts={"device": "cpu"},
    )

    print(f"[overlap] carregando faster-whisper ({args.model}, cpu)...")
    from faster_whisper import WhisperModel
    wm = WhisperModel(args.model, device="cpu", compute_type="int8")

    def transcribe_stem(audio):
        """ASR de uma voz separada → (texto, ini, fim, palavras) ou None."""
        import tempfile
        import os
        with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tmp:
            sf.write(tmp.name, audio, sr)
            try:
                segs2, _ = wm.transcribe(
                    tmp.name, language=args.lang, beam_size=5,
                    vad_filter=True, word_timestamps=True,
                    condition_on_previous_text=False,
                )
                words = [w for s in segs2 for w in (s.words or [])]
            finally:
                os.unlink(tmp.name)
        if not words:
            return None
        text = " ".join(w.word.strip() for w in words).strip()
        if not text:
            return None
        return {
            "text": text,
            "start": float(words[0].start),
            "end": float(words[-1].end),
        }

    splits = []
    skipped = 0
    for k, s in enumerate(cand):
        a = int(s["start"] * sr)
        b = int(s["end"] * sr)
        chunk = data[a:b]
        if len(chunk) < sr * args.min_dur:
            continue

        # pré-filtro barato: silêncio interno > 0.3s = fala única
        # (2 vozes sobrepostas não têm pausas internas claras)
        ws = int(sr * 0.010)
        nf = len(chunk) // ws
        if nf > 10:
            frames = chunk[:nf * ws].reshape(nf, ws)
            rms = np.sqrt(np.mean(frames ** 2, axis=1))
            thr = max(rms.max() * 0.02, 0.0008)
            on = rms > thr
            gap_max = 0.0
            gs = None
            for fi in range(nf):
                if not on[fi]:
                    if gs is None:
                        gs = fi
                else:
                    if gs is not None:
                        gl = (fi - gs) * ws / sr
                        if gl > gap_max:
                            gap_max = gl
                        gs = None
            if gs is not None:
                gl = (nf - gs) * ws / sr
                if gl > gap_max:
                    gap_max = gl
            if gap_max > 0.3:
                skipped += 1
                continue

        try:
            with torch.no_grad():
                wav = torch.from_numpy(chunk).unsqueeze(0)
                est = sep.separate_batch(wav).cpu().numpy()  # (1, sources, samples)
            if est.ndim == 3:
                est = est[0]
            if est.ndim == 2 and est.shape[1] == 2 and est.shape[0] != 2:
                est = est.T  # alguns builds retornam (samples, sources)
        except Exception as e:
            print(f"[overlap] falha SepFormer seg {s['id']}: {e}")
            continue

        parts = []
        stems_rms = []
        for stem in est:
            rms = float(np.sqrt(np.mean(stem ** 2)))
            stems_rms.append(rms)
            if rms < 1e-4:
                continue
            tr = transcribe_stem(stem / (rms + 1e-9))
            if tr is None:
                continue
            span = tr["end"] - tr["start"]
            if span < args.min_part:
                continue
            parts.append({
                "id": s["id"] * 1000 + len(parts) + 1,
                "start": round(s["start"] + tr["start"], 3),
                "end": round(s["start"] + tr["end"], 3),
                "text": tr["text"],
                "prob": s.get("prob", 0),
                "no_speech": 0.0,
                "words": [],
            })

        # SepFormer separa até fala única: o stem secundário é resíduo/eco da
        # mesma voz. Filtra por (a) energia relativa e (b) texto não duplicado.
        if len(parts) >= 2:
            mx = max(stems_rms)
            n_strong = len([r for r in stems_rms if r >= 1e-4])
            ratios = [r / mx for r in stems_rms if r >= 1e-4]
            if n_strong >= 2 and min(ratios) >= args.min_energy_ratio:
                import difflib
                t0_, t1_ = parts[0]["text"], parts[1]["text"]
                sim = difflib.SequenceMatcher(None, t0_, t1_).ratio()
                if sim < args.max_sim:
                    splits.append({"orig_id": s["id"], "parts": parts})
                    print(f"[overlap] seg {s['id']} ({s['start']:.1f}-{s['end']:.1f}s) "
                          f"→ {len(parts)} vozes: "
                          + " | ".join(f"[{p['start']:.1f}-{p['end']:.1f}] {p['text'][:28]}"
                                       for p in parts))
                else:
                    print(f"[overlap] seg {s['id']} sem split (mesma voz, sim={sim:.2f})")
            else:
                print(f"[overlap] seg {s['id']} sem split (energia fraca)")

        if (k + 1) % 25 == 0:
            el = time.time() - t0
            print(f"[overlap] {k+1}/{len(cand)} candidatos "
                  f"({el:.0f}s, {len(splits)} splits até agora)")

    with open(args.out, "w", encoding="utf-8") as f:
        json.dump({"splits": splits}, f, ensure_ascii=False, indent=1)
    print(json.dumps({
        "candidates": len(cand), "skipped_silence": skipped,
        "splits": len(splits),
        "seconds": round(time.time() - t0, 1),
    }))


if __name__ == "__main__":
    main()
