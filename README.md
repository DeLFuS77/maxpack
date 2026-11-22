# MaxPack

Herramienta para que un juego de PS5 (dump propio o homebrew) ocupe lo mínimo posible y siga siendo
jugable. El diseño completo está en
[docs/diseno/maxpack.md](docs/diseno/maxpack.md).

**Estado: fases 0 y 1.**
- `maxpack analyze` mide cuánto gana cada técnica de compresión en un juego concreto.
- `maxpack pack` lo empaquetará en packs zstd que ampr-emu-ng sirve en la consola: en una carpeta
  lista para copiar a la PS5 o en un único `.p5z` para guardar en el PC.
- `unpack`, `verify` e `info` completan el ciclo: el `.p5z` devuelve el dump original bit a bit.

`maxpack` nunca escribe en el juego de origen y no toca claves, cifrado ni DRM.

## Compilar

Hace falta git, CMake 3.20 o posterior y un compilador de C. zstd 1.5.6 y zlib 1.3.1 vienen como
submódulos, así que hay que clonar con `--recursive` (o ejecutar `git submodule update --init`
después de clonar).

```
git clone --recursive <url-del-repositorio> maxpack
cd maxpack
```

### Windows

Visual Studio 2022/2026 (o Build Tools) con la carga de trabajo **Desarrollo para el escritorio con
C++**, **incluido el componente Windows 11 SDK**: sin el SDK, CMake dice "No CMAKE_C_COMPILER could be
found". También hace falta Python 3 para las pruebas.

```
cmake -S . -B build
cmake --build build --config Release
ctest --test-dir build -C Release --output-on-failure
```

El ejecutable queda en `build\Release\maxpack.exe`.

### Linux

```
sudo apt install build-essential cmake python3
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j
ctest --test-dir build --output-on-failure
```

### macOS

```
xcode-select --install
brew install cmake python
cmake -S . -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build -j
ctest --test-dir build --output-on-failure
```

> Linux y macOS todavía no se han compilado ni probado; el código está escrito para ellos (POSIX y
> pthreads), pero de momento solo está verificado en Windows con MSVC.

## Uso

```
maxpack analyze <carpeta|imagen.exfat> [opciones]

  --sample N%        porcentaje de la muestra (por defecto 2 %, mínimo 1 GiB)
  --full             medir todo el juego, sin muestra
  --json ARCHIVO     guardar el informe en JSON
  --threads N        hilos (por defecto, todos los de la CPU)
  --keep-loose GLOB  dejar suelto también lo que case (se puede repetir); '*' y '?' dentro de una
                     carpeta, '**' cualquier número de carpetas. Ej.: --keep-loose "**/*.bnk"
  --no-dedup         no hacer el pase de deduplicación (que lee todo el juego)
  --no-transport     no medir el .p5z de transporte (zstd 19 con ventana larga)
  --list             solo listar los archivos y si irían sueltos o empaquetados
```

`--json` nunca sobrescribe el juego que se analiza ni escribe dentro de su carpeta: si apunta ahí, o
a una carpeta que no existe, `maxpack` se niega antes de empezar.

Ejemplo:

```
maxpack analyze "D:\juegos\MiJuego-PPSA00000.exfat" --json informe.json
```

El progreso sale por stderr y el informe por stdout. Si el origen es un PKG, de momento lo rechaza
con un aviso: los PKG llegarán en la fase 4.

## Empaquetar, guardar y verificar

```
maxpack pack <carpeta|imagen.exfat> -o <carpeta> [--level N] [--runtime libSceAmpr.sprx] [--threads N]
maxpack pack <carpeta|imagen.exfat> -o <juego.p5z> --p5z [--level N] [--runtime ...]
maxpack unpack <juego.p5z> --folder -o <carpeta>      # la carpeta para la consola
maxpack unpack <juego.p5z> --original -o <carpeta>    # el dump original, bit a bit
maxpack info <juego.p5z>
maxpack verify <juego.p5z | carpeta> [--against <origen>]
```

- **`pack --folder`** (por defecto) produce la carpeta que se copia a la consola:
  - los sueltos tal cual;
  - los packs `ampr_zstd-NNN.pak` y `ampr_zstd.index` (formato `AMPRZPK1` de ampr-emu-ng, bloques de
    256 KiB, nivel 19 por defecto);
  - un `ampr_emu.index` nuevo.
- **`pack --p5z`** guarda lo mismo en un solo archivo:
  - los packs, tal cual;
  - lo suelto, comprimido con zstd 19 y ventana larga;
  - el SHA-256 de cada archivo original.

  Formato en [docs/formato-p5z.md](docs/formato-p5z.md).
- **El runtime de la consola** tiene que ser el `libSceAmpr.sprx` de **ampr-emu-ng**: el ampr_emu
  antiguo no lee packs zstd. Con `--runtime` se coloca en `fakelib/`. Si el runtime final no parece
  leer zstd, `pack` lo avisa.
- **Garantías de seguridad:**
  - nunca escribe en el origen;
  - no acepta una salida que no esté vacía;
  - se niega con juegos ya empaquetados o con rutas que solo se diferencian en mayúsculas;
  - `ampr_zstd.index` se escribe el último, así que un empaquetado cortado no se monta;
  - un `.p5z` cortado o dañado se detecta al abrirlo.
- **Misma entrada, mismos bytes:** con las mismas opciones, el resultado es idéntico byte a byte
  con cualquier número de hilos.
- **Comprobaciones:**
  - `verify` comprueba cada archivo por SHA-256 (`.p5z`), o los packs y los índices (carpeta);
  - con `--against <origen>` compara además la carpeta con el juego original;
  - `unpack --original` vuelve a comprobar cada SHA-256 al reconstruir.

Para probar un juego empaquetado en la consola: [docs/probar-en-ps5.md](docs/probar-en-ps5.md).

## Cómo leer el informe

- **Suelto** es lo que la consola necesita como archivo normal: ejecutables y módulos (`eboot.bin`,
  `*.elf`, `*.self`, `*.prx`, `*.sprx`), `*.ini`, `sce_sys/`, `sce_module/`, `fakelib*/` y los archivos
  de AMPR. **Empaquetable** es el resto.
- **Muestra**: se comprime una muestra determinista de ventanas de 1 MiB (por defecto el 2 %, mínimo
  1 GiB) y se extrapola por tipo de archivo (extensión). Con `--full` se mide todo y el resultado es
  exacto. Misma entrada y mismas opciones dan las mismas cifras con cualquier número de hilos.
- **Configuraciones**:
  - *v1 nivel 5 / 9 / 19 / 22*: packs compatibles con ampr-emu-ng (`AMPRZPK1`), bloques de 256 KiB.
    El nivel 5 es el que usa Dump Forge por defecto. Se aplica la regla de Forge: un bloque que no
    mejora va en bruto (como Forge, ya no se guarda en bruto el archivo entero si ahorra poco).
  - *v2 bloque de 1 MiB* y *v2 diccionario*: mejoras que necesitarían un runtime nuevo (M7 y M6 del
    diseño). El diccionario se entrena con una mitad de los datos y se mide con la otra. Con
    cualquier diccionario, zstd renuncia a la doble pasada del primer bloque que hace en el nivel 19
    (cuesta un 0,75–0,9 %); es un coste real de usar diccionario y por eso está incluido.
  - *zlib 64 KiB*: referencia aproximada de `.ffpfsc` (no cuenta su tabla de bloques).
- **Total en consola** = sueltos + empaquetado (+ índices `AMPRZPK1`/`AMPRIDX3` en v1).
  **Ahorro** = 1 − total en consola / tamaño original.
- **MB/s/hilo** y **Tiempo est.** son la velocidad de compresión medida y el tiempo estimado de
  empaquetar todo el juego con los hilos usados.
- **Deduplicación** (M4): bloques de 256 KiB y archivos idénticos, por XXH64, sobre todo el juego.
- **Archivos < 64 KiB** (M5): comprimidos uno a uno (como v1), agrupados en bloques sólidos de
  256 KiB, o uno a uno con diccionario.
- **.p5z estimado**: lo que ocuparía el archivo único para el PC con v1 nivel 19 y los sueltos
  comprimidos con zstd 19 y ventana larga. Se instala copiando los packs tal cual.
- **Transporte**: lo que ocuparía un `.p5z` pensado solo para guardar en el PC, con todo lo
  empaquetable comprimido como los sueltos (zstd 19, ventana larga, frames de 32 MiB). Ocupa menos,
  pero al instalar la PS5 tendría que descomprimirlo y volver a comprimirlo en v1, lo que alarga
  mucho la instalación. Se mide sobre una muestra de tramos de 32 MiB.

Los informes de los juegos analizados están en [docs/informes/](docs/informes/).

## Pruebas

`ctest` genera imágenes exFAT sintéticas (`tests/tools/make_exfat.py`) con nombres largos y con emoji,
archivos fragmentados, ValidDataLength menor que el tamaño, imágenes truncadas y una FAT con bucle.
`tools/exfat_ref.py` es un lector exFAT independiente, en Python, para contrastar el lector en C con
imágenes reales (`tests/tools/compare_ranges.py`).

## Licencia

GPL-3.0-or-later. Ver [LICENSE](LICENSE).
