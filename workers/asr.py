#!/usr/bin/env python3
"""
workers/asr.py — ASR (faster-whisper) wrapper fino.

Transcreve o áudio em japonês e devolve segmentos com timestamps de
palavra + VAD. É a base da "janela de tempo" de cada fala original
(regra de ouro 4: não atropelar e não acelerar a fala dublada).

Uso (chamado pelo orquestrador Go):
    .venv/bin/python workers/asr.py --audio work/vocals.wav --lang ja \
        --model small --out work/asr.json
"""
import argparse
import json
import time


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--audio", required=True)
    ap.add_argument("--lang", default="ja")
    ap.add_argument("--model", default="small")
    ap.add_argument("--out", required=True)
    ap.add_argument("--compute-type", default="int8")
    ap.add_argument("--min-silence-ms", type=int, default=400)
    ap.add_argument("--beam", type=int, default=5)
    args = ap.parse_args()

    from faster_whisper import WhisperModel

    t0 = time.time()
    print(f"[asr] carregando modelo {args.model} (cpu, {args.compute_type})...")
    model = WhisperModel(args.model, device="cpu", compute_type=args.compute_type)

    segments, info = model.transcribe(
        args.audio,
        language=args.lang,
        beam_size=args.beam,
        vad_filter=True,
        vad_parameters=dict(min_silence_duration_ms=args.min_silence_ms),
        word_timestamps=True,
        condition_on_previous_text=False,
        without_timestamps=False,
    )

    out = []
    for i, s in enumerate(segments):
        words = [
            {
                "w": w.word.strip(),
                "start": round(w.start, 3),
                "end": round(w.end, 3),
                "p": round(w.probability, 3),
            }
            for w in (s.words or [])
        ]
        out.append({
            "id": i,
            "start": round(s.start, 3),
            "end": round(s.end, 3),
            "text": s.text.strip(),
            "prob": round(s.avg_logprob, 3),
            "no_speech": round(s.no_speech_prob, 3),
            "words": words,
        })

    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(
            {"info": {"language": info.language, "duration": info.duration},
             "segments": out},
            f, ensure_ascii=False, indent=1,
        )
    print(json.dumps({"n": len(out), "seconds": round(time.time() - t0, 1)}))


if __name__ == "__main__":
    main()
