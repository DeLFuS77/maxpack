# Formato `.p5z` (versión 1)

Un `.p5z` es un juego de PS5 ya empaquetado en un único archivo para guardarlo en el PC. Contiene:
- la carpeta que irá a la consola: los packs zstd tal cual, más los sueltos y los índices comprimidos;
- los datos para reconstruir el dump original bit a bit: el manifiesto con el SHA-256 de cada archivo
  y los originales que la carpeta reemplaza.

Lo escribe `maxpack pack --p5z` y lo leen `unpack`, `verify` e `info`. Todos los enteros son
little-endian y el CRC es CRC-32 IEEE (el de zlib).

## Estructura

```
[cabecera 64 B][datos ...][índice: un frame zstd][pie 32 B]
```

### Cabecera (64 B)

| off | tipo | campo |
|---|---|---|
| 0 | [8] | `MAXPKP5Z` |
| 8 | u32 | versión = 1 |
| 12 | u32 | tamaño de cabecera = 64 |
| 16 | u32 | flags = 0 |
| 20 | u8 | nivel de zstd de los packs |
| 21 | [3] | 0 |
| 24 | [16] | archive_id (el de `ampr_zstd.index`; ceros si no hay packs) |
| 40 | [20] | 0 |
| 60 | u32 | CRC de la cabecera con los bytes 60..64 a cero |

### Datos

Regiones seguidas desde el byte 64, en el orden en que las produce el empaquetado:
1. archivos sueltos, en orden de ruta;
2. volúmenes `ampr_zstd-NNN.pak`;
3. `ampr_emu.index`;
4. `fakelib/libSceAmpr.sprx` de `--runtime`;
5. originales reemplazados;
6. `ampr_zstd.index`.

Cómo se guarda cada región:
- **Volúmenes:** en bruto, idénticos al archivo de la carpeta; instalar es copiar.
- **Todo lo demás:** frames zstd independientes de hasta 32 MiB, con nivel 19, ventana 2^25,
  long-distance matching y checksum.

### Pie (32 B)

| off | tipo | campo |
|---|---|---|
| 0 | [8] | `P5ZFOOT1` |
| 8 | u64 | desplazamiento del índice |
| 16 | u64 | longitud comprimida del índice |
| 24 | u32 | CRC del índice comprimido |
| 28 | u32 | CRC de los bytes 0..28 del pie |

El índice termina justo donde empieza el pie. Como el pie se escribe el último, un `.p5z` cortado se
reconoce.

### Índice (sin comprimir)

Cabecera del índice (56 B):

| off | tipo | campo |
|---|---|---|
| 0 | [8] | `P5ZIDX01` |
| 8 | u32 | versión = 1 |
| 12 | u32 | n_files (archivos guardados) |
| 16 | u32 | n_frames |
| 20 | u32 | n_manifest (archivos del dump original) |
| 24 | u64 | longitud de las cadenas |
| 32 | u32, u32 | desplazamiento y longitud del ID de título en las cadenas |
| 40 | u32, u32 | ídem, versión del contenido |
| 48 | u32, u32 | ídem, nombre del título |

Le siguen, sin huecos, `files`, `frames`, `manifest` y las cadenas.

**files** (88 B por registro):

| off | tipo | campo |
|---|---|---|
| 0 | u32, u32 | ruta: desplazamiento y longitud en las cadenas |
| 8 | u8 | papel: 0 suelto, 1 volumen, 2 `ampr_zstd.index`, 3 `ampr_emu.index`, 4 runtime, 5 original reemplazado |
| 9 | u8 | almacenamiento: 0 en bruto, 1 frames |
| 10 | u16 | 0 |
| 12 | u64 | tamaño |
| 20 | i64 | mtime (segundos Unix) |
| 28 | u64 | inicio de la región |
| 36 | u64 | longitud de la región |
| 44 | u32 | primer frame (en `frames`) |
| 48 | u32 | número de frames |
| 52 | [32] | SHA-256 del contenido; en un volumen, de sus bytes [64, fin), porque la cabecera tiene su propio CRC |
| 84 | u32 | 0 |

**frames** (16 B): u64 desplazamiento · u32 tamaño comprimido · u32 tamaño sin comprimir.

**manifest** (64 B, uno por archivo del dump original, en orden de ruta):

| off | tipo | campo |
|---|---|---|
| 0 | u32, u32 | ruta |
| 8 | u32 | flags: bit0 empaquetado (está en los packs), bit1 reemplazado (en `files` con papel 5) |
| 12 | u32 | índice en `files` (0xFFFFFFFF si está empaquetado) |
| 16 | u64 | tamaño |
| 24 | i64 | mtime |
| 32 | [32] | SHA-256 |

## Reconstrucción

- **`unpack --folder`:** escribe los archivos de `files` salvo los de papel 5. Es la carpeta para la
  consola, igual a `pack --folder`.
- **`unpack --original`:** reconstruye cada registro del manifiesto, comprobando su SHA-256:
  - empaquetado: se decodifica de los packs;
  - suelto: es el archivo de `files` que indica el registro;
  - reemplazado: es su original de papel 5.

## Jugar sin instalar (consola)

La consola puede jugar un `.p5z` sin instalarlo, y sin dejar en el disco interno más que lo imprescindible.

- **Lo que se extrae:** solo los sueltos, es decir, los archivos de papel 0 (sueltos del juego), 2
  (`ampr_zstd.index`) y 3 (`ampr_emu.index`). Cada uno se escribe entero, con su fecha, y se comprueba su SHA-256.
  Nunca se extraen los volúmenes (papel 1), el runtime (papel 4) ni los originales (papel 5).
- **Lo que no se extrae:** los volúmenes `ampr_zstd-NNN.pak` se leen en su sitio, dentro del `.p5z`, que es
  donde están guardados sin comprimir. Cada volumen empieza en `offset` (el `data_off` de su registro) y mide
  `length` bytes, cabecera de 64 B incluida.
- **Abrir:** antes de jugar se comprueba la cabecera, el pie, el índice, `ampr_zstd.index`, la cabecera de cada
  volumen y que el `archive_id` de la cabecera del `.p5z` sea el de `ampr_zstd.index`.
- **Qué lee la consola:** `maxpack info --play <juego.p5z>` escribe en JSON el título, la versión, el
  `archive_id`, el tamaño del `.p5z`, los sueltos (`loose`, con `loose_bytes` como suma) y los volúmenes
  (`volumes`, con `id`, `offset` y `length`). En C, lo mismo está en `mp_play_open`, `mp_play_json` y
  `mp_play_extract` (`src/pack/mp_play.h`).
- **`maxpack.image`:** con esos datos el payload escribe el archivo `maxpack.image` que lee `ampr-emu-ng` para
  localizar los volúmenes dentro del `.p5z`. Su contrato está en `docs/p5z-mode.md` de ampr-emu-ng.

