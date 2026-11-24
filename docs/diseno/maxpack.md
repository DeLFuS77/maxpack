# MaxPack — diseño

*Nombre provisional. 22 de noviembre de 2026.*

## 1. Intención

Que un juego de PS5 (dump propio o homebrew) ocupe **lo mínimo posible y siga siendo jugable**,
tanto guardado en el PC como dentro de la consola, sin descomprimirlo nunca a tamaño completo.

Por qué: los juegos ocupan mucho y el almacenamiento está caro. Hoy la mejor opción (packs zstd
de PS5 Dump Forge + ampr-emu-ng) ya baja un juego de RE Engine de 90,6 GB a 38,4 GB, pero se
queda corta en varios puntos que se pueden mejorar (sección 4).

## 2. Objetivos, no-objetivos y anti-objetivos

**Objetivos**
- O1. Máxima compresión **jugable** en PS5 (SSD interno, M.2 o USB), cargando con ShadowMountPlus.
- O2. El archivo que guardas en el PC es el mismo juego comprimido; al meterlo en la consola sigue
  comprimido (solo se descomprime la pequeña parte que el sistema exige suelta).
- O3. Recuperación bit a bit: del archivo se puede reconstruir el dump original, verificado con
  SHA-256 por archivo (preservación).
- O4. Herramienta de PC para Windows, Linux y macOS, e instalador/gestor en la PS5.
- O5. Decidir con datos: un analizador mide, por juego, cuánto gana cada técnica antes de aplicarla.

**No-objetivos**
- Tocar claves, cifrado o DRM; crear o firmar PKG; consolas sin jailbreak; juegos de PS4 (de momento).
- Interfaz gráfica en las primeras fases (primero CLI en PC y web en la PS5).

**Anti-objetivos (sería un fracaso aunque "funcione")**
- Que un juego comprimido dé tirones o se cuelgue: la compresión no puede romper la jugabilidad.
- Un formato que solo lea nuestra herramienta y no se pueda devolver a carpeta normal.
- Un fork del runtime que diverja y muera: los cambios se proponen a sus autores.

## 3. Restricciones (de la investigación)

- Ejecutables y módulos (`eboot.bin`, `*.elf`, `*.self`, `*.prx`, `*.sprx`), `sce_sys/`, `sce_module/`,
  `fakelib*/` y los `.ini` tienen que quedar **sueltos** en la consola: los carga el sistema.
- Un archivo que el juego abre con `mmap` no se puede empaquetar (el runtime no lo sirve).
- Runtime de stock (ampr-emu-ng, formato `AMPRZPK1`): bloque fijo de 256 KiB, ventana ≤ 256 KiB,
  sin diccionario, bloques obligatoriamente consecutivos (no admite deduplicación), nivel 1–22,
  volúmenes ≤ 4.294.901.760 bytes, y necesita `ampr_emu.index` (`AMPRIDX3`) listando todos los archivos.
- Trampa de compatibilidad: `AMPRZPK1` usa FNV-1a 64 con la base estándar
  (14695981039346656037) sobre la ruta relativa respetando mayúsculas; `AMPRIDX3` usa la base
  **1469598103934665603** (la de `build_ampr_index.py`) sobre `/app0/...` en minúsculas, y `h or 1`.
  Mezclarlas ya rompió juegos en exFAT2AMPR.
- Montar una imagen `.exfat` lee a unos 350 MB/s; una carpeta es más rápida.
- El runtime comparte CPU y memoria con el juego (pool de unos 32 MiB más el índice).
- En este entorno solo puedo compilar C (clang 18, que es lo que pide el SDK de PS5) y no puedo
  probar en consola: las pruebas en PS5 las haces tú.

## 4. Mejoras sobre lo existente

| # | Mejora | Necesita runtime nuevo | Ganancia esperada |
|---|---|---|---|
| M1 | Nivel 19 en vez de 5 (zstd descomprime igual de rápido) | No | algunos % (a medir) |
| M2 | Empaquetar todo lo empaquetable (reglas de Forge "todo menos sistema") | No | depende del juego |
| M3 | Archivo único en PC con la parte suelta comprimida (zstd 19, ventana larga) | No | lo que pese lo suelto |
| M4 | Deduplicación de bloques y archivos idénticos | Sí | 0–30 % según juego |
| M5 | Agrupar archivos pequeños en bloques sólidos | Sí | alta en juegos con >100k archivos |
| M6 | Diccionario entrenado por juego | Sí | sobre todo en archivos pequeños |
| M7 | Bloque adaptable por archivo (64 KiB–1 MiB según patrón de lectura) | Sí | pocos %, con riesgo de latencia |

Las mejoras de runtime (M4–M7) **solo se implementan si el analizador demuestra que compensan**.

## 5. Enfoques considerados

- **A. Solo compatible:** packs `AMPRZPK1` al máximo + archivo de PC + instalador. Funciona hoy con
  ampr-emu-ng y Kraken Mount, pero se queda en M1–M3.
- **B. A + formato v2 y runtime mejorado, por fases.** ← **Elegido** por ti. Da la máxima compresión
  jugable; cuesta más trabajo y pruebas en consola.
- **C. Contribuir a Dump Forge / ampr-emu-ng** sin app propia. Descartado como camino principal
  (no controlas resultado ni plazos), pero las mejoras de runtime se propondrán upstream igualmente.

## 6. Diseño

### 6.1 Piezas

1. **Núcleo `libmaxpack` (C11)** — formatos, empaquetado, hashes (SHA-256, CRC-32, FNV-1a, XXH64),
   lectores de origen (carpeta e imagen exFAT; después UFS2/`.ffpkg`, PFS/`.ffpfsc` y PKG).
   zstd va incluido como submódulo (facebook/zstd). Mismo código para PC y PS5.
2. **CLI `maxpack` (PC)** — `analyze`, `pack`, `unpack`, `verify`, `info`.
3. **Payload PS5 `maxpack.elf`** (C, ps5-payload-sdk + zstd de pacbrew-repo) — interfaz web,
   instala `.p5z` y registra el juego en ShadowMountPlus. Hoy lo publica DeLFuS77/maxpack-ps5, que usa
   las bibliotecas de este repositorio (`src/install/`, `src/web/`).
4. **Runtime v2** — versión modificada de ampr-emu-ng (Rust, GPL-3) que lee también el formato v2.

### 6.2 Analizador (fase 0)

`maxpack analyze <carpeta|imagen.exfat> [--sample 2%] [--full] [--json informe.json]`

- Recorre el juego y separa lo que debe ir suelto de lo empaquetable (mismas reglas que 6.3).
- Comprime una muestra determinista de bloques (por defecto 2 %, mínimo 1 GiB; `--full` lo hace
  todo) y extrapola. Mide: tamaño total/suelto/empaquetable, número de archivos y distribución de
  tamaños; packs v1 a nivel 5 (lo de Forge), 9, 19 y 22; bloques de 256 KiB contra 1 MiB;
  duplicados exactos (bloques de 256 KiB y archivos enteros); agrupación de archivos < 64 KiB;
  diccionario entrenado; y como referencia zlib en bloques de 64 KiB (≈ `.ffpfsc`).
- Informa de la velocidad de compresión medida y estima el tiempo total de cada opción.

### 6.3 Empaquetado compatible v1 (fase 1)

`maxpack pack <origen> -o <salida> [--level 19] [--runtime libSceAmpr.sprx] [--p5z | --folder]`

- **Suelto siempre:** archivos vacíos, `eboot.bin` (en cualquier carpeta), `*.elf`, `*.self`,
  `*.prx`, `*.sprx`, `*.ini`, todo `sce_sys/`, `sce_module/`, `fakelib*/` de la raíz, y los
  archivos AMPR (`ampr_emu.index`, `ampr_zstd.index`, `ampr_zstd-*.pak`, `ampr_assets*`, logs).
  Reglas extra de suelto/empaquetado con `--keep-loose <glob>` (por si un juego usa `mmap`).
- **Empaquetado:** el resto, en bloques de 256 KiB, cada uno un frame zstd independiente (tamaño
  de contenido en la cabecera, checksum activado, ventana ≤ 2^18, sin diccionario). Si el frame no
  es más pequeño, el bloque va en bruto; si el archivo ahorra < 1 %, todo el archivo va en bruto.
- Salida idéntica en formato a la de Dump Forge: `ampr_zstd.index` (`AMPRZPK1`),
  `ampr_zstd-NNN.pak` (`AMPRZDT1`) y `ampr_emu.index` (`AMPRIDX3`) generado a partir de la lista
  **original** de archivos (sueltos y empaquetados, con su tamaño y fecha originales).
- El resultado no depende del número de hilos: misma entrada, mismos bytes.
- Si se pasa `--runtime`, se copia a `fakelib/libSceAmpr.sprx`.

### 6.4 Archivo de PC `.p5z` (fase 1)

Un único archivo con el juego ya empaquetado:

- Volúmenes `.pak` **guardados tal cual** (ya están comprimidos), para que instalarlos sea copiar.
- Archivos sueltos y los índices comprimidos con zstd 19 y ventana larga, en frames de hasta
  32 MiB (paralelizables y con acceso aleatorio).
- Metadatos: `param.json`, `icon0.png`, ID de título, versión, y un manifiesto con ruta, tamaño,
  fecha y SHA-256 de **cada archivo del dump original**.
- Índice al final y pie con su posición y CRC (se escribe en una pasada). Cabecera con versión.
- `maxpack unpack juego.p5z --original` reconstruye el dump original y verifica cada SHA-256;
  `--folder` deja la carpeta empaquetada lista para la consola.

### 6.5 Instalador en PS5 (fase 2)

- Payload ELF con interfaz web (por ejemplo en el puerto 8095).
- Origen: `.p5z` en USB o en el almacenamiento interno; después SMB (libsmb2) y envío directo
  desde el PC (`maxpack send --to <ip>`).
- Instala en `/data/homebrew/<TITLE_ID>` o en un USB: copia los `.pak`, descomprime lo suelto, coloca
  `libSceAmpr.sprx` en `fakelib/`, verifica y lo registra con la API local de ShadowMountPlus
  (`/api/v1/manual/add` y `/api/v1/scan`), dejando desactivada la actualización de fakelib para ese juego.
- Comprueba espacio libre antes de empezar; reanuda si se interrumpe.

### 6.6 Formato v2 y runtime (fase 3, condicionada a los datos)

- Formato propio `MXPKIDX1` / `MXPKDAT1` (`maxpack.index`, `maxpack-NNN.pak`) para no chocar con
  el espacio de nombres de ampr-emu-ng.
- Registros de bloque que pueden apuntar al mismo tramo físico (deduplicación, M4).
- Grupos sólidos de archivos pequeños: el registro indica grupo, desplazamiento y longitud (M5).
- Diccionario opcional por pack, cargado una vez como `ZSTD_DDict` (M6).
- Tamaño de bloque por archivo, de 2^16 a 2^20 (M7).
- Runtime: fork de ampr-emu-ng que lee v1, v2, LZ4 y PKG; la lectura por lotes se parte cuando
  los bloques no son contiguos. Se propone upstream cuando esté probado en consola.
- Solo se activan las mejoras que el analizador haya justificado.

### 6.7 Fases

| Fase | Entrega | Quién prueba |
|---|---|---|
| 0 | `maxpack analyze` + binario de Linux + instrucciones para compilar en Windows/macOS | Tú, en tu PC con un `.exfat` |
| 1 | `pack` v1 compatible, `.p5z`, `unpack` bit a bit, `verify` | Yo (round-trip) + tú en PS5 con SMP + ampr-emu-ng |
| 2 | Instalador PS5 | Tú en consola |
| 3 | Formato v2 + runtime | Tú en consola, juego a juego |
| 4 | GUI, lectura de `.ffpkg`/`.ffpfsc`/PKG, CI con binarios para los 3 sistemas, propuestas upstream | — |

### 6.8 Verificación

- Pruebas unitarias de cada formato con vectores conocidos (por ejemplo, hashes FNV-1a con ambas bases).
- Pruebas de ida y vuelta sobre dumps sintéticos: datos aleatorios y comprimibles, duplicados,
  cientos de miles de archivos pequeños, archivos de más de 4 GiB y cambio de volumen (con un límite
  pequeño en pruebas). Comprobación: SHA-256 idénticos.
- Contraste con lectores ajenos: la salida v1 debe abrirse con `ps5-dump-forge inspect` y con
  ampr-emu-ng en consola.
- Lista de comprobación en consola: arranque, vídeo, carga, guardado y 10 minutos de juego, sin errores.

## 7. Decisiones que he tomado yo (puedes cambiarlas)

- Lenguaje C11 + CMake para PC y PS5 (verificable aquí y compartido); Rust solo para el runtime.
- Licencia GPL-3.0-or-later (la de ampr-emu-ng, ShadowMountPlus y Dump Forge).
- Nombre "MaxPack" y extensión `.p5z`.
- Nivel por defecto 19; el 22 queda disponible, porque con ventana de 256 KiB apenas gana.
- Salida por defecto en carpeta (más rápida en consola); imagen `.exfat` como opción más adelante.
- Entrada `.exfat` en la fase 0; PKG más adelante (necesita descodificar Kraken).

## 8. Lo que queda al criterio del constructor

Nombres de opciones de la CLI, pool de hilos, formato exacto del informe JSON, detalles de la
interfaz web y la estructura interna de los módulos.
