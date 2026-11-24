# Notas de investigación (22-11-2026)

Resumen técnico de lo estudiado para el diseño de MaxPack. Todo se ha leído del código o de la
documentación de cada proyecto; las cifras son las que publican sus autores.

## 1. Estado de la escena

- Jailbreak: Relapse (WebKit → kernel) para firmware 7.00–13.60, PS5 y PS5 Pro. 14.00 no es
  compatible. Hay un kernel exploit por arranque (no es permanente).
- Pila habitual: ELF loader en el puerto 9021 (elfldr, etaHEN, Payload Manager) + kstuff-lite
  + ShadowMountPlus (SMP) 1.7.
- fPKG en PS5: kstuff-lite hasta 11.60; ps5upload 5.41 dice que los fPKG de PS4 y PS5 instalan y
  arrancan en 13.60. Está cambiando.

## 2. Formatos jugables y herramientas

| Formato | Qué es | Notas |
|---|---|---|
| Carpeta (dump) | archivos descifrados + fself | lo más rápido |
| `.exfat` | imagen exFAT | SMP la monta (LVD o MD); clusters de 64 KiB; unos 350 MB/s medidos |
| `.ffpkg` | imagen UFS2 | recomendada por SMP |
| `.ffpfs` | imagen PFS | experimental |
| `.ffpfsc` | PFSC: zlib en bloques de 64 KiB | el kernel la lee; 150–250 MB/s; −47 % en un homebrew de 6,5 GB |
| Packs LZ4 (`AMPRPAK4`) | ampr_emu (drakmor) | deduplicación, bloques de 16 KiB a 1 MiB |
| Packs zstd (`AMPRZPK1`) | ampr-emu-ng (quer3q) | bloque de 256 KiB, nivel 1–22 (Forge usa 5 por defecto) |
| fPKG debug + Kraken | Dump Forge, ps5upload, UltraPack | Kraken Mount los monta sin instalar |

Dato medido (docs de ampr-emu-ng): un juego de RE Engine pasa de 90,6 GB a 38,4 GB en packs zstd
de nivel 5, jugable, con 0 errores.

Herramientas: PS5 Dump Forge (Rust; convierte cualquier formato en otro, también en PS5),
ps5upload, MkPFS (Python, `.ffpfsc`), exFAT2AMPR (LZ4 en consola), PS5 Game Compressor,
PS5 UltraPack, Kraken Mount (monta fPKG y carpetas con packs), ShadowMountPlus.

## 3. `AMPRZPK1` (packs zstd de ampr-emu-ng)

Todo little-endian. CRC-32 IEEE. Ficheros: `ampr_zstd.index` en la raíz del juego y
`ampr_zstd-000.pak`, `-001.pak`… (al menos 3 dígitos, máximo 4096 volúmenes de 4.294.901.760 bytes
cada uno, cabecera incluida).

Hash de ruta: FNV-1a 64, base **14695981039346656037**, primo 1099511628211, sobre la ruta relativa
en UTF-8, con `/`, sin barra inicial y **respetando mayúsculas**. Ruta no vacía, sin `\` ni NUL, sin
partes vacías, `.` ni `..`, menor de 1024 bytes. Ninguna ruta puede ser carpeta de otra.

Cabecera del índice (128 bytes):

| off | tipo | campo |
|---|---|---|
| 0 | [8] | `AMPRZPK1` |
| 8 | u32 | versión = 1 |
| 12 | u32 | tamaño de cabecera = 128 |
| 16 | u8 | chunk_shift = 18 (otro valor se rechaza) |
| 17 | u8 | nivel zstd 1..22 (otro valor se rechaza) |
| 18 | u16 | 0 |
| 20 | u32 | volume_count |
| 24 | [16] | archive_id aleatorio (se repite en cada volumen) |
| 40 | u64 | file_count |
| 48 | u64 | chunk_count (< 2^32) |
| 56 | u64 | files_off (= 128) |
| 64 | u64 | chunks_off |
| 72 | u64 | volumes_off |
| 80 | u64 | strings_off |
| 88 | u64 | strings_len (el índice acaba en strings_off + strings_len) |
| 96 | u64 | bytes lógicos totales empaquetados |
| 104 | u64 | 0 |
| 112 | u32 | CRC de [128, fin) |
| 116 | u32 | CRC de la cabecera con 112..120 a cero |
| 120 | u64 | 0 |

Las tablas van seguidas sin huecos: archivos, bloques, volúmenes y cadenas. chunk_count y
volume_count son los dos 0 o los dos distintos de 0.

Registro de archivo (32 bytes), ordenado por (path_hash, bytes de la ruta), sin duplicados:
u64 path_hash · u64 logical_size · u32 first_chunk (suma de bloques previos; bloques =
ceil(tamaño / 256 KiB), 0 si vacío) · u32 path_off · u16 path_len · u16 flags (bit0 = store-only) ·
u32 0.

Registro de bloque (u64): bits 0..31 desplazamiento absoluto en el volumen (≥ 64) · 32..43 id de
volumen · 44..61 stored_size − 1 · 62..63 códec (0 bruto, 1 zstd).

Registro de volumen (u64): tamaño del fichero del volumen con cabecera.

Cabecera de volumen (64 bytes): `AMPRZDT1` · u32 versión 1 · u32 64 · u32 id · u32 volume_count ·
[16] archive_id · u64 bytes de payload (tamaño − 64) · [12] 0 · u32 CRC de la cabecera con 60..64 a
cero.

Reglas que valida el lector (`crates/ampr-format/src/zstd.rs`): los bloques van **seguidos**, en el
orden del índice, desde el offset 64; un archivo puede continuar en el volumen siguiente; cada
volumen acaba donde termina su último bloque y tiene al menos uno. El bloque i descomprime a
min(262144, tamaño − i·262144). Un bloque zstd es un frame con content size, checksum activado, sin
diccionario y ventana ≤ 256 KiB; un bloque en bruto guarda exactamente su tamaño, y uno zstd es
estrictamente menor. Archivo con ahorro < 1 % → store-only (todo en bruto).

El runtime cruza cada registro con `ampr_emu.index`: cada archivo empaquetado debe estar ahí con la
misma ruta (salvo mayúsculas) y el mismo tamaño; si no, el montaje se invalida.

## 4. `AMPRIDX3` (`ampr_emu.index`)

Generado por `tools/build_ampr_index.py` (drakmor/ampr_emu). Lista todos los archivos del juego.

- Ruta: `/app0/` + ruta relativa con `/`. Clave = ruta con A–Z pasadas a minúsculas (solo ASCII).
- Hash: FNV-1a 64 con base **1469598103934665603** (la de este script, NO la estándar), primo
  1099511628211, sobre la clave; si sale 0 se usa 1.
- Filas ordenadas por clave.
- Cabecera `<8sIIQQQII`: `AMPRIDX3`, versión 3, tamaño de registro (24), número de filas, longitud
  del blob de rutas, offset de la tabla hash, tamaño de slot (16), número de slots.
- Registro `<IIQq`: offset en el blob, longitud de ruta, tamaño, mtime (segundos Unix).
- Blob: rutas UTF-8 terminadas en NUL. Relleno hasta múltiplo de 16 (build_ampr_index.py). Después, los slots. Al leer se acepta también el offset de los slots alineado solo a 8, como el de otros generadores (el índice propio de un paquete); al reescribir, siempre a 16.
- Slots: potencia de 2 ≥ 2·filas (mínimo 2), direccionamiento abierto lineal desde `h & mask`;
  slot `<QII>` = (hash, índice+1, flags); flag 1 = hash duplicado (se marca en ambos).
- Se excluyen `ampr_commands.bin` y `apr_emu.log`.
- exFAT2AMPR 1.87 arregló juegos que no arrancaban por usar la base estándar aquí.

## 5. Lo que sirve el runtime (ampr-emu-ng)

- Se instala como `fakelib/libSceAmpr.sprx` del juego. Con SMP hay que desactivar la actualización
  de fakelib. Kraken Mount lo instala él mismo.
- Sirve AMPR/APR, `open/read/pread/readv/preadv/lseek/fstat/close/stat/getdents` (también stdio)
  y la AIO del kernel sobre archivos empaquetados. **No sirve `mmap`.**
- Siempre sueltos según Forge: archivos vacíos, `eboot.bin`, `*.elf`, `*.self`, `*.prx`, `*.sprx`,
  `*.ini`, `sce_sys/`, `sce_module/`, `fakelib*/` de la raíz, archivos AMPR y los propios packs.
- Memoria: pool de 32 MiB (release) y ledger del índice de 80 MiB; índices hasta 512 MiB. Un juego
  medido tenía 242.409 archivos.
- Lecturas sueltas: 8 hilos de pread de 256 KiB llegan a ~2 GB/s en SSD interno; la AIO del kernel
  se queda en ~730 MB/s.

## 6. ShadowMountPlus (API local)

HTTP/JSON en `127.0.0.1:10101` (o `0.0.0.0`), sin autenticación; todo `POST` con JSON salvo `GET /`.
Rutas útiles: `/api/v1/version`, `/api/v1/manual/add {"path":...}`, `/api/v1/scan`,
`/api/v1/games`, `/api/v1/games/fakelib {"title_id":..,"mode":..}`, `/api/v1/settings/update`
(incluye `update_emulators`, `auto_update_ampr`), `/api/v1/games/mount`, `/api/v1/packages/install`.
Rutas de escaneo por defecto: `/data/homebrew`, `/data/etaHEN/games`, `/mnt/ext0..1`, `/mnt/usb0..7`
(y sus `homebrew/` y `etaHEN/games/`). Las imágenes deben tener los archivos del juego en la raíz.
El instalador nativo pide rangos (offset, tamaño) en desorden cuando instala un PKG por streaming.

## 7. SDK de PS5

- ps5-payload-dev/sdk (John Törnblom, GPL-3): clang-18 + lld-18. Ejemplos: `install_app`
  (`sceAppInstUtilInitialize`, `sceAppInstUtilAppInstallTitleDir`), `notify` (`sceNotificationSend`).
- ps5-payload-dev/pacbrew-repo tiene portados zstd 1.5.6, lz4, xz, zlib, libsmb2, libnfs,
  libmicrohttpd, curl, mbedtls/openssl, sqlite y libarchive.

## 8. NSZ (Switch), como referencia

NCZ = NCA descifrada y comprimida con zstd. Los primeros 0x4000 bytes quedan iguales, seguidos de
una cabecera de secciones y, opcionalmente, una cabecera de bloques para acceso aleatorio. El
instalador vuelve a cifrar y recrea la NCA bit a bit. Lección: NSZ se adoptó porque era simple;
NSPZ era demasiado complejo y nadie lo implementó.

## 9. Repositorios

- https://github.com/quer3q/ampr-emu-ng — runtime zstd (docs/zstd-format.md)
- https://github.com/quer3q/ps5-dump-forge — escritor de packs (docs/zstd.md, docs/packing.md)
- https://github.com/quer3q/kraken-mount
- https://github.com/drakmor/ampr_emu — LZ4 y AMPRIDX3 (docs/ASSET_PACKS.md, tools/build_ampr_index.py)
- https://github.com/drakmor/ShadowMountPlus — docs/api.md
- https://github.com/PSBrew/MkPFS
- https://github.com/phantomptr/ps5upload
- https://github.com/exFAT2AMPR/exfat2ampr
- https://github.com/juma-sayeh/PS5-Game-Compressor
- https://github.com/knutwurst/ps5-ultrapack
- https://github.com/ps5-payload-dev/sdk y https://github.com/ps5-payload-dev/pacbrew-repo
- https://github.com/nicoboss/nsz — docs/formats.md
