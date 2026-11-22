# Fase 1: verificación con juegos reales (22-11-2026)

Windows 11, Ryzen 9 7950X3D (32 hilos), NVMe. `maxpack` 0.0.1 con zstd 1.5.6, nivel 19 (el
predeterminado). Los tiempos son de reloj; algunos coincidieron con compilaciones en paralelo.

## Hollow Knight: Silksong (PPSA12544, 2.165 archivos, 8,39 GB)

| Paso | Resultado |
|---|---|
| `pack --folder` | 95 s · 2.119 empaquetados: 8,17 GB → 2,25 GB en 1 volumen (−72,4 %) |
| `pack --p5z` | 128 s · `.p5z` de 2,33 GB |
| `ampr_emu.index` generado | **idéntico byte a byte** al que trae el dump (hecho con `build_ampr_index.py`) |
| `verify` carpeta `--against` imagen | 2.163 correctos, 0 fallos (3 s) |
| `verify` `.p5z` | 2.168 correctos, 0 fallos (7 s) |
| `tools/ampr_ref.py zpk-check` (lector independiente en Python) | 2.119 archivos, 0 errores |
| `unpack --folder` | idéntico a `pack --folder` |
| `unpack --original` (29 s) + SHA-256 independiente en Python | 2.165 archivos idénticos a la imagen |
| Analizador (muestra de 1 GiB) | estimó 2,31 GB de packs: +2,7 % sobre lo real |

## RESIDENT EVIL requiem (PPSA31246, 49 archivos, 133,59 GB)

Tiene archivos de hasta 72 GB, que cruzan varios volúmenes de 4 GiB.

| Paso | Resultado |
|---|---|
| `pack --p5z` | 1.456 s · 21 empaquetados: 133,23 GB → 79,18 GB en 19 volúmenes (−40,6 %) · `.p5z` de 79,32 GB |
| `verify` `.p5z` | 70 correctos, 0 fallos (291 s): cada archivo guardado y cada empaquetado contra su SHA-256 original |
| Analizador (muestra del 2 %) | estimó 79,87 GB de packs: +0,9 % sobre lo real |

## Pendiente

- La prueba en consola con el `libSceAmpr.sprx` de ampr-emu-ng (ver
  [probar-en-ps5.md](../probar-en-ps5.md)). Los runtimes que traen Silksong y Wukong son el
  ampr_emu antiguo y no leen packs zstd.
- Contraste con un lector ajeno (`ps5-dump-forge inspect`): opcional, necesita descargarlo.
- Compilar y probar en Linux y macOS.
