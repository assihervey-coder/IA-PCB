"""NetTokenizer: tokenization of net names / pad references.

Special tokens: ``<pad>=0`` (padding), ``<unk>=1`` (unknown), ``<bos>`` and
``<eos>`` (sequence boundaries). The vocabulary is stored as a plain JSON
map ``token -> id`` (see ``vocab.json``) and can be extended with
:meth:`NetTokenizer.build`.
"""

from __future__ import annotations

import json
import os
from typing import Dict, Iterable, List, Optional, Sequence

PAD, UNK, BOS, EOS = "<pad>", "<unk>", "<bos>", "<eos>"
SPECIAL_TOKENS: Sequence[str] = (PAD, UNK, BOS, EOS)


class NetTokenizer:
    """Bidirectional token <-> id mapping for net names.

    Args:
        max_len: maximum encoded sequence length (excluding BOS/EOS).
    """

    def __init__(self, max_len: int = 16) -> None:
        self.max_len = int(max_len)
        self.token_to_id: Dict[str, int] = {}
        self.id_to_token: Dict[int, str] = {}
        self._reset_vocab()

    def _reset_vocab(self) -> None:
        """(Re)initialize the vocabulary with the special tokens."""
        self.token_to_id = {token: idx for idx, token in enumerate(SPECIAL_TOKENS)}
        self.id_to_token = {idx: token for token, idx in self.token_to_id.items()}

    # ------------------------------------------------------------------ build

    def build(self, corpus: Iterable[str]) -> "NetTokenizer":
        """Add every token of ``corpus`` to the vocabulary (idempotent)."""
        for name in corpus:
            for token in self._tokenize(name):
                self._add(token)
        return self

    @staticmethod
    def _tokenize(name: str) -> List[str]:
        """Split a net name into character-level tokens (upper-case)."""
        return list(str(name).upper())

    def _add(self, token: str) -> None:
        if token not in self.token_to_id:
            index = len(self.token_to_id)
            self.token_to_id[token] = index
            self.id_to_token[index] = token

    # ---------------------------------------------------------------- encode

    def encode(self, name: str, add_specials: bool = True) -> List[int]:
        """Encode a net name into a list of ids.

        The sequence is truncated to ``max_len``; unknown characters map to
        ``<unk>``. ``<bos>`` / ``<eos>`` wrap the sequence when
        ``add_specials`` is True.
        """
        tokens = self._tokenize(name)[: self.max_len]
        ids = [self.token_to_id.get(token, self.token_to_id[UNK]) for token in tokens]
        if add_specials:
            ids = [self.token_to_id[BOS]] + ids + [self.token_to_id[EOS]]
        return ids

    def decode(self, ids: Iterable[int]) -> str:
        """Decode ids back into a string (special tokens are skipped)."""
        specials = {self.token_to_id[t] for t in SPECIAL_TOKENS}
        return "".join(
            self.id_to_token.get(int(i), "") for i in ids if int(i) not in specials
        )

    # ------------------------------------------------------------ properties

    def __len__(self) -> int:
        return len(self.token_to_id)

    @property
    def pad_id(self) -> int:
        """Id of ``<pad>``."""
        return self.token_to_id[PAD]

    @property
    def unk_id(self) -> int:
        """Id of ``<unk>``."""
        return self.token_to_id[UNK]

    # ----------------------------------------------------------- persistence

    def save(self, path: str) -> bool:
        """Save the vocabulary as JSON (``{"token": id}`` + max_len)."""
        try:
            os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
            payload = {
                "max_len": self.max_len,
                "vocab": dict(sorted(self.token_to_id.items(), key=lambda kv: kv[1])),
            }
            with open(path, "w", encoding="utf-8") as handle:
                json.dump(payload, handle, ensure_ascii=False, indent=2)
            return True
        except Exception:
            return False

    def load(self, path: str) -> bool:
        """Load a JSON vocabulary; returns True on success."""
        try:
            with open(path, "r", encoding="utf-8") as handle:
                payload = json.load(handle)
            vocab = payload.get("vocab", payload) if isinstance(payload, dict) else {}
            self.max_len = int(payload.get("max_len", self.max_len)) if isinstance(payload, dict) else self.max_len
            self._reset_vocab()
            for token, index in sorted(vocab.items(), key=lambda kv: int(kv[1])):
                self.token_to_id[token] = int(index)
                self.id_to_token[int(index)] = token
            return True
        except Exception:
            return False


if __name__ == "__main__":
    # Demo: load the starter vocabulary, encode/decode a few net names,
    # then extend the vocabulary with unseen names and save it.
    here = os.path.dirname(os.path.abspath(__file__))
    tokenizer = NetTokenizer(max_len=16)
    vocab_path = os.path.join(here, "vocab.json")
    if tokenizer.load(vocab_path):
        print(f"vocabulaire charge : {len(tokenizer)} tokens depuis {vocab_path}")
    else:
        print("vocab.json introuvable, demarrage d'un vocabulaire vierge")
        tokenizer.build(["GND", "VCC", "CLK", "SDA", "SCL", "TX", "RX"])

    for sample in ["GND", "CLK", "VCC", "DATA7"]:
        ids = tokenizer.encode(sample)
        print(f"  encode({sample!r}) = {ids} -> decode = {tokenizer.decode(ids)!r}")

    tokenizer.build(["DATA7", "CS0"])
    print(f"vocabulaire etendu : {len(tokenizer)} tokens")
    out_path = os.path.join(here, "vocab_extended.json")
    if tokenizer.save(out_path):
        print(f"vocabulaire sauvegarde : {out_path}")
