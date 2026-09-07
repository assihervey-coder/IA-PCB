"""Board vision transformer (BoardViT) over multi-channel grid observations.

The board raster ``(C, H, W)`` is split into fixed patches by a strided
Conv2d "patch embedding", prepended with a learnable CLS token plus position
embedding, then processed by a standard ``nn.TransformerEncoder``.

Outputs of :meth:`BoardViT.forward`:
    ``(cls_emb, patch_tokens)`` where ``cls_emb`` is ``(B, d_model)`` and
    ``patch_tokens`` is ``(B, N, d_model)``.

An optional per-patch ``route_head`` (Linear -> 1) is exposed through
:meth:`BoardViT.forward_route` for patch-level routability prediction.

torch is imported lazily: importing this module never requires torch. The
classes are built on first use via the module-level ``__getattr__`` (PEP 562).
"""

from __future__ import annotations

import math

_LAZY_CLASSES: dict | None = None


def _torch():
    """Import and return the torch module lazily."""
    import torch

    return torch


def _lazy_classes() -> dict:
    """Build (once) the PatchEmbedding / BoardViT torch classes."""
    global _LAZY_CLASSES
    if _LAZY_CLASSES is None:
        torch = _torch()
        nn = torch.nn
        F = torch.nn.functional

        class PatchEmbedding(nn.Module):
            """Conv2d patch embedding (patch_size x patch_size, stride equal)."""

            def __init__(self, in_channels: int, d_model: int = 256, patch_size: int = 16) -> None:
                super().__init__()
                self.patch_size = int(patch_size)
                self.proj = nn.Conv2d(
                    in_channels, d_model, kernel_size=patch_size, stride=patch_size
                )

            def forward(self, x):  # (B, C, H, W) -> (B, N, d_model)
                patches = self.proj(x)
                return patches.flatten(2).transpose(1, 2)

        class BoardViT(nn.Module):
            """Vision transformer over board rasters.

            Args:
                in_channels: number of observation channels.
                img_size: reference raster size used to size the positional
                    embedding (``ceil(img_size / patch_size) ** 2`` patches;
                    interpolated dynamically beyond that).
                patch_size: spatial patch size (Conv2d kernel/stride).
                dim: transformer width (d_model).
                depth: number of encoder layers.
                heads: attention heads.
                use_route_head: add the per-patch routability head.
                dropout: transformer dropout.
            """

            def __init__(
                self,
                in_channels: int = 5,
                img_size: int = 128,
                patch_size: int = 16,
                dim: int = 256,
                depth: int = 6,
                heads: int = 8,
                use_route_head: bool = True,
                dropout: float = 0.0,
            ) -> None:
                super().__init__()
                grid = max(1, math.ceil(float(img_size) / float(patch_size)))
                max_patches = grid * grid
                self.patch_embed = PatchEmbedding(in_channels, dim, patch_size)
                self.d_model = int(dim)
                self.max_patches = int(max_patches)
                self.cls_token = nn.Parameter(torch.zeros(1, 1, dim))
                self.pos_embedding = nn.Parameter(torch.zeros(1, max_patches + 1, dim))
                nn.init.trunc_normal_(self.pos_embedding, std=0.02)
                nn.init.trunc_normal_(self.cls_token, std=0.02)
                encoder_layer = nn.TransformerEncoderLayer(
                    d_model=dim,
                    nhead=heads,
                    dim_feedforward=4 * dim,
                    dropout=dropout,
                    batch_first=True,
                )
                self.encoder = nn.TransformerEncoder(encoder_layer, num_layers=depth)
                self.norm = nn.LayerNorm(dim)
                self.route_head = nn.Linear(dim, 1) if use_route_head else None

            def _add_position(self, tokens):
                """Prepend CLS and add (interpolated) position embeddings."""
                batch, n_patches, dim = tokens.shape
                cls = self.cls_token.expand(batch, 1, dim)
                tokens = torch.cat([cls, tokens], dim=1)
                pos = self.pos_embedding
                if tokens.shape[1] > pos.shape[1]:
                    pos = F.interpolate(
                        pos.transpose(1, 2),
                        size=tokens.shape[1],
                        mode="linear",
                        align_corners=False,
                    ).transpose(1, 2)
                else:
                    pos = pos[:, : tokens.shape[1]]
                return tokens + pos

            def forward(self, x) -> tuple[torch.Tensor, torch.Tensor]:
                tokens = self.patch_embed(x)
                hidden = self.norm(self.encoder(self._add_position(tokens)))
                return hidden[:, 0], hidden[:, 1:]

            def forward_route(self, x) -> torch.Tensor:
                """Per-patch routeability logits ``(B, N)`` (head required)."""
                if self.route_head is None:
                    raise RuntimeError("BoardViT was built with use_route_head=False")
                _cls, patch_tokens = self.forward(x)
                return self.route_head(patch_tokens).squeeze(-1)

        _LAZY_CLASSES = {"PatchEmbedding": PatchEmbedding, "BoardViT": BoardViT}
    return _LAZY_CLASSES


def __getattr__(name: str):
    """PEP 562: expose PatchEmbedding / BoardViT lazily (torch required)."""
    if name in ("PatchEmbedding", "BoardViT"):
        return _lazy_classes()[name]
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


def __dir__():  # pragma: no cover - introspection helper
    return sorted(set(globals()) | {"PatchEmbedding", "BoardViT"})
