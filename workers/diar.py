#!/usr/bin/env python3
"""
workers/diar.py — diarização (falante por fala) wrapper fino.

Para cada segmento de fala do ASR, calcula um embedding de voz e
agrupa em clusters (personagens). A identidade/nome é atribuída
depois em Go (perfil de voz + roteiro).

Embedding primário: speechbrain ECAPA-TDNN (Apache-2.0).
Fallback (sem download): MFCC stats via librosa.

Uso:
    .venv/bin/python workers/diar.py --audio work/vocals.wav \
        --segments work/asr.json --out work/diar.json
"""
import argparse
import json
import time


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--audio", required=True)
    ap.add_argument("--segments", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--max-clusters", type=int, default=12)
    ap.add_argument("--embedder", default="auto")  # auto|ecapa|mfcc
    args = ap.parse_args()

    import numpy as np
    import soundfile as sf

    t0 = time.time()
    with open(args.segments, encoding="utf-8") as f:
        segs = json.load(f)["segments"]

    data, sr = sf.read(args.audio, dtype="float32", always_2d=True)
    data = data.mean(axis=1)
    data16, sr16 = data, sr
    if sr != 16000:
        import librosa
        data16 = librosa.resample(data, orig_sr=sr, target_sr=16000)
        sr16 = 16000

    def slice_seg(s):
        a = int(s["start"] * sr16)
        b = int(s["end"] * sr16)
        return data16[a:b]

    # ── embedder ─────────────────────────────────────────────────────
    use_ecapa = False
    if args.embedder in ("auto", "ecapa"):
        try:
            import torch
            from speechbrain.pretrained import EncoderClassifier
            clf = EncoderClassifier.from_hparams(
                source="speechbrain/spkrec-ecapa-voxceleb",
                run_opts={"device": "cpu"},
            )
            use_ecapa = True
        except Exception as e:
            print(f"[diar] ECAPA indisponível ({e}); usando MFCC fallback")

    import librosa

    def embed_mfcc(audio):
        if len(audio) < sr16 * 0.2:
            return None
        mfcc = librosa.feature.mfcc(y=audio, sr=sr16, n_mfcc=13)
        return np.concatenate([mfcc.mean(1), mfcc.std(1)]).astype("float32")

    def embed_ecapa(audio):
        if len(audio) < sr16 * 0.4:
            return None
        import torch
        with torch.no_grad():
            emb = clf.encode_batch(
                torch.from_numpy(audio).unsqueeze(0),
                torch.tensor([sr16]),
            )
        return emb.squeeze().numpy()

    method = "ecapa" if use_ecapa else "mfcc"
    vecs, ids = [], []
    for s in segs:
        if use_ecapa:
            try:
                e = embed_ecapa(slice_seg(s))
            except Exception as exc:
                print(f"[diar] ECAPA falhou em runtime ({exc}); usando MFCC")
                use_ecapa = False
                method = "mfcc"
                vecs, ids = [], []
        if not use_ecapa:
            e = embed_mfcc(slice_seg(s))
        if e is None:
            continue
        vecs.append(e)
        ids.append(s["id"])

    n = len(vecs)
    if n == 0:
        with open(args.out, "w") as f:
            json.dump({"method": "none", "assign": {}, "n_clusters": 0}, f)
        print(json.dumps({"n": 0, "seconds": round(time.time() - t0, 1)}))
        return

    X = np.stack(vecs)
    X = X / (np.linalg.norm(X, axis=1, keepdims=True) + 1e-9)

    # ── clustering aglomerativo (cosine) c/ silhouette ───────────────
    from sklearn.cluster import AgglomerativeClustering
    from sklearn.metrics import silhouette_score

    best_k, best_score, best_labels = 1, -1.0, np.zeros(n, dtype=int)
    for k in range(2, min(args.max_clusters, n) + 1):
        if k >= n:
            break
        try:
            cl = AgglomerativeClustering(
                n_clusters=k, metric="cosine", linkage="average"
            ).fit(X)
        except Exception:
            break
        sc = silhouette_score(X, cl.labels_, metric="cosine")
        if sc > best_score:
            best_score, best_k, best_labels = sc, k, cl.labels_

    assign = {int(i): int(c) for i, c in zip(ids, best_labels)}
    with open(args.out, "w", encoding="utf-8") as f:
        json.dump({
            "method": method,
            "n_clusters": int(best_k),
            "silhouette": round(float(best_score), 3),
            "assign": assign,
        }, f, ensure_ascii=False, indent=1)
    print(json.dumps({
        "n": n, "n_clusters": int(best_k),
        "silhouette": round(float(best_score), 3),
        "seconds": round(time.time() - t0, 1),
    }))


if __name__ == "__main__":
    main()
