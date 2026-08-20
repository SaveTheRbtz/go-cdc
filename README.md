# Content Defined Chunking playground

This repository provides Go implementations of RepMaxCDC "Repeated Maximum"
and several related [Content-Defined
Chunking](https://en.wikipedia.org/wiki/Rolling_hash) algorithms. RepMaxCDC is
[one of the standard CDC functions of Bazel's remote execution protocol](https://github.com/bazelbuild/remote-apis/pull/282).
An implementation written in Java
[is part of Bazel](https://github.com/bazelbuild/bazel/pull/30131).

RepMaxCDC provides:

- **Tight chunk size bounds:** Most CDC functions generate chunks
  whose minimum and maximum size are still a factor of 16 or 32 apart.
  RepMaxCDC is capable of generating chunks with sizes in range
  $[n, 2n)$, while offering excellent deduplication rates.

- **Excellent parallelism:** RepMaxCDC allows performing targeted
  searches for cutting points. This makes it possible to partition a
  large file into roughly equally sized pieces. These can be chunked in
  parallel.

- **Size-based checking:** With chunks always falling in range $[n, 2n)$,
  it is trivial to check whether a file can be split into multiple
  chunks, purely looking at its size. This property, which other CDC
  functions often lack, was needed to add support for chunking to
  Bazel's existing remote execution protocol in a backward compatible
  way.

Two Gear-based implementations of RepMaxCDC are included:

- [`simple_rep_max_content_defined_chunker.go`](/simple_rep_max_content_defined_chunker.go):
  A very simple, but inefficient implementation that hashes input data
  repeatedly.

- [`rep_max_chunk_reader.go`](/rep_max_chunk_reader.go): The optimized
  RepMaxCDC engine shared by the Gear, polynomial, and lexicographic variants.
  It avoids redundant score computation on the normal path by retaining a
  small frontier of record maxima across calls. A concrete internal ordering
  configuration selects the specialized scanner once per region, keeping each
  hot loop direct.

Tests are used to validate that both implementations yield the same
results.

`NewPolyRepMaxContentDefinedChunker()` selects the polynomial variant of the
shared RepMaxCDC engine. Its fixed 64-byte rolling hash modulo $2^{64}$ lives
in [`polynomial_hash.go`](/polynomial_hash.go). It retains the state reuse and
targeted-search support of the optimized Gear implementation, but produces
different chunk boundaries.

The package also contains several hashless CDC families:

- `NewLexicographicRepMaxContentDefinedChunker()` applies the RepMaxCDC
  selection rule to fixed-size byte strings in unsigned lexicographic order.
  It is based on the [hash-less local maxima construction described by
  Bjørner, Blass, and Gurevich](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/tr-2007-102.pdf).

- `NewAsymmetricExtremumContentDefinedChunker()` implements byte-wise
  [AE-Max](https://cswxia.github.io/pub/AE-INFOCOM-zhang-2015.pdf).
  `NewWideAsymmetricExtremumContentDefinedChunker()` extends the same rule to
  overlapping 1-, 2-, 4-, or 8-byte regions. Regions use explicit unsigned
  little-endian ordering, making the x86 ordering of the public
  [WideCDC artifact](https://github.com/UWASL/dedup-bench/tree/artifact_fast27)
  deterministic on every architecture. The one-byte form is exactly AE-Max.

- `NewRAMContentDefinedChunker()` implements canonical, uncapped
  [Rapid Asymmetric Maximum](https://doi.org/10.1016/j.future.2017.02.013).
  `NewRAMLContentDefinedChunker()` is the separately named length-limited
  variant, with the [evaluated RAML cap of four times the window
  size](https://tsukuba.repo.nii.ac.jp/record/2005453/files/DA010258.pdf).

- `NewSeqContentDefinedChunker()` implements increasing-mode
  [SeqCDC](https://sreeharshau.github.io/papers/SeqCDC_Middleware24.pdf) with
  the paper's parameter sets targeting 4, 8, and 16 KiB averages. This
  implementation interprets increasing sequences strictly and includes the
  terminal byte, rather than reproducing the equality and length conventions
  of the reference C++ implementation.

The AE, WideAE, RAM, RAML, SeqCDC, and lexicographic RepMax variants do not
support `DiscardUpToGuaranteedChunk()`. Canonical RAM is intentionally uncapped
and may produce very large chunks on structured input; RAML is the bounded
alternative for applications that require predictable memory use.

Builds made with `GOEXPERIMENT=simd` use Go's experimental SIMD packages. On
amd64, the polynomial scanner uses `simd/archsimd`; other architectures use
the portable `simd` package. Builds without the experiment use the scalar
scanner. All implementations produce identical boundaries. The amd64
implementation requires an AVX2-capable processor; it performs no runtime CPU
detection.

This repository also contains a copy of a paper titled
["Content-Defined Chunking with tight chunk size bounds"](/papers/cdc.pdf),
which provides a formal description and analysis of RepMaxCDC. It also
describes some simpler algorithms on which RepMaxCDC is based: MaxCDC
("Maximum") and RecMaxCDC ("Recursive Maximum"). This paper can be
referenced as follows:

```bibtex
@misc{repmaxcdc,
      title = {Content-Defined Chunking with tight chunk size bounds},
      author = {Ed Schouten},
      year = {2026},
      month = aug,
      url = {https://github.com/buildbarn/go-cdc/blob/main/papers/cdc.pdf},
}
```
