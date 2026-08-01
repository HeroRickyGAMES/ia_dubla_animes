#!/usr/bin/env python3
"""
workers/sep.py — separação voz × fundo (demucs htdemucs) wrapper fino.

Regra de ouro 5: a voz isolada vira a base de clonagem de cada
personagem (dublagem "mais clean"); o fundo (outras stems) vira o
"bed" do mix com ducking — fluxo melhor.

Uso:
    .venv/bin/python workers/sep.py --audio work/audio48k.wav \
        --vocals work/vocals.wav --bg work/nv.wav
"""
import argparse
import json
import time


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--audio", required=True)
    ap.add_argument("--vocals", required=True)
    ap.add_argument("--bg", required=True)
    ap.add_argument("--model", default="htdemucs")
    ap.add_argument("--segment", type=float, default=4.0)
    args = ap.parse_args()

    import torch
    from demucs.audio import AudioFile, save_audio
    from demucs.pretrained import get_model
    from demucs.apply import apply_model

    t0 = time.time()
    print(f"[sep] carregando modelo {args.model} (cpu)...")
    model = get_model(args.model)
    model.to("cpu")
    model.eval()

    wav = AudioFile(args.audio).read(
        streams=0, samplerate=model.samplerate, channels=model.audio_channels
    )
    if wav.ndim == 1:
        wav = wav[None]
    print(f"[sep] áudio {float(wav.shape[-1]) / float(model.samplerate):.1f}s "
          f"@ {int(model.samplerate)}Hz")

    ref = wav.mean(0)
    wav = (wav - ref.mean()) / (ref.std() + 1e-8)

    with torch.inference_mode():
        sources = apply_model(
            model, wav[None], device="cpu",
            shifts=0, split=True, overlap=0.25, segment=args.segment,
            progress=True,
        )[0]

    sources = sources * ref.std() + ref.mean()
    vocals = sources[model.sources.index("vocals")]
    rest = sum(sources[i] for i in range(len(model.sources))
               if model.sources[i] != "vocals")

    save_audio(vocals, args.vocals, model.samplerate)
    save_audio(rest, args.bg, model.samplerate)

    print(json.dumps({
        "seconds": round(time.time() - t0, 1),
        "sr": model.samplerate,
        "vocals": args.vocals,
        "bg": args.bg,
    }))


if __name__ == "__main__":
    main()
