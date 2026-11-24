<!-- SPDX-License-Identifier: GPL-3.0-or-later -->
# Guía de MaxPack

MaxPack comprime juegos de PS5 (tu dump, en carpeta o en imagen `.exfat`) para que ocupen menos y se
puedan jugar sin descomprimirlos. Esta guía va de principio a fin: descargar MaxPack, comprimir un
juego, llevarlo a la PS5 y jugarlo. Cada sección resume y enlaza la página con los detalles.

> **Antes de empezar:** MaxPack todavía no se ha probado en una PS5 real. En el PC se prueba en cada
> cambio, pero que un juego comprimido arranque en la consola está por comprobar
> ([sección 8](#8-qué-está-probado-y-qué-falta)). Guarda siempre tu dump original.

![La ventana de MaxPack](img/ventana-claro.jpg)

## Índice

1. [Qué es MaxPack y qué necesitas](#1-qué-es-maxpack-y-qué-necesitas)
2. [El camino rápido](#2-el-camino-rápido)
3. [Descargar e instalar el programa del PC](#3-descargar-e-instalar-el-programa-del-pc)
4. [Comprimir, Descomprimir, Verificar y Detalles](#4-comprimir-descomprimir-verificar-y-detalles)
5. [En la PS5](#5-en-la-ps5)
6. [La línea de comandos](#6-la-línea-de-comandos)
7. [Problemas frecuentes](#7-problemas-frecuentes)
8. [Qué está probado y qué falta](#8-qué-está-probado-y-qué-falta)
9. [Licencias y créditos](#9-licencias-y-créditos)

## 1. Qué es MaxPack y qué necesitas

MaxPack guarda el juego en packs zstd (formato `AMPRZPK1`). En la consola los lee **ampr-emu-ng**
mientras juegas, así que el juego nunca se descomprime entero. Solo quedan sin comprimir los archivos
que el sistema necesita sueltos: ejecutables y módulos, `sce_sys/`, `fakelib/` y poco más.

Como referencia, en el PC Hollow Knight: Silksong pasó de 8,39 GB a un `.p5z` de 2,33 GB, y
RESIDENT EVIL requiem, de 133,59 GB a 79,32 GB ([informe](informes/fase1.md)).

MaxPack tiene tres piezas:

| Pieza | Dónde | Para qué |
|---|---|---|
| **MaxPack**, la ventana | PC | Comprimir, descomprimir, verificar y ver los detalles de un juego, sin escribir comandos. |
| **`maxpack`**, el motor | PC | Hace el trabajo. Va junto a la ventana con el nombre `maxpack-core`, y también se usa solo, como [línea de comandos](#6-la-línea-de-comandos). |
| **`maxpack.elf`**, el instalador | PS5 | Instala tus `.p5z`, que siguen comprimidos en la consola, desde una página web en el puerto 8095. También instala tal cual carpetas e imágenes. |

Hay dos formas de llevar un juego a la PS5. En la ventana se eligen en **Hecho para**:
- **Instalador MaxPack** (lo normal): un archivo `.p5z` por juego. Es lo más pequeño y se guarda bien
  en el PC o en un USB. En la PS5, el instalador lo deja listo para jugar.
- **Jugar desde el USB**: una carpeta por juego, ya lista para la consola, sin instalador.

En la consola, las dos acaban igual: la misma carpeta, comprimida, en `homebrew/`.

### Qué necesitas

**En el PC**
- Windows de 64 bits, macOS 13 o posterior (con chip de Apple o Intel) o Linux de 64 bits con GTK 3 y
  WebKitGTK 4.1.
- Tu juego: un dump de PS5 en carpeta o en imagen `.exfat`. Los PKG, `.ffpkg`, `.ffpfs` y `.ffpfsc`
  todavía no se pueden comprimir.
- El `libSceAmpr.sprx` de ampr-emu-ng: el de la copia de MaxPack,
  [DeLFuS77/ampr-emu-ng](https://github.com/DeLFuS77/ampr-emu-ng), que se descarga de sus Releases
  (mientras no haya versiones publicadas, es el artefacto `sprx` de su GitHub Actions). Sin él, el
  juego comprimido no arranca con ShadowMountPlus. La v0.5.0 original de quer3q también lee los packs
  zstd. Algunos dumps traen un `libSceAmpr.sprx` antiguo que no lee zstd: MaxPack pone el de
  ampr-emu-ng en `fakelib/` de los juegos que no lo traen.
- Espacio en el destino. MaxPack avisa si hay menos libre de lo que ocupa el juego original.

**En la PS5**
- Una PS5 con jailbreak y un cargador de ELF en el puerto 9021 (elfldr, etaHEN o Payload Manager).
- Para jugar, ShadowMountPlus (con kstuff, la pila habitual) o Kraken Mount.
- Un USB en **exFAT** si algún `.p5z` pasa de 4 GiB: FAT32 no admite archivos tan grandes. Al
  formatear un USB se borra lo que tenga.
- Para abrir la página del instalador, el navegador de la PS5, o un móvil o un PC de tu red.

> MaxPack nunca escribe en el juego de origen y no toca claves, cifrado ni DRM. En la consola nunca
> comprime nada: comprimir es cosa del PC.

## 2. El camino rápido

### Con el instalador MaxPack (lo normal)

1. **Descarga** MaxPack para tu sistema y `maxpack.elf` para la PS5
   ([sección 3](#3-descargar-e-instalar-el-programa-del-pc)). Instala MaxPack y ábrelo.
2. **Pon ampr-emu-ng.** Descarga `libSceAmpr.sprx` de las Releases de la
   [copia de ampr-emu-ng de MaxPack](https://github.com/DeLFuS77/ampr-emu-ng). En MaxPack, abre **Ajustes** y, en «Runtime
   para los juegos que no lo traen», pulsa **Elegir libSceAmpr.sprx…**. MaxPack lo recuerda.
   - Si te lo saltas, MaxPack lo pide una vez antes de empezar, con los juegos que lo necesitan.
   - Hoy este paso es obligatorio para el instalador: `maxpack.elf` no lleva ningún ampr-emu-ng dentro
     y no instala un `.p5z` que no lo traiga.
3. **Comprime.** En **Comprimir**, suelta tus juegos en la ventana (o usa **Añadir archivos** y
   **Añadir carpeta**). En **Hecho para**, deja **Instalador MaxPack**. Elige el **Destino**: el USB
   (en exFAT) o una carpeta del PC. Pulsa **Comprimir**. Cada juego se verifica al terminar y queda
   como `<Título> [<TITLE_ID>].p5z`.
4. **Lleva los `.p5z` al USB**, si no los guardaste ya ahí. Déjalos en la raíz del USB o en una
   carpeta justo debajo: el instalador no busca más adentro.
5. **Carga el instalador.** Conecta el USB a la PS5 y envía `maxpack.elf` a tu cargador de ELF
   (puerto 9021), como cualquier payload. Cuando la consola diga «MaxPack … listo», abre
   `http://<IP de la PS5>:8095` en el navegador de la PS5, del móvil o del PC.
6. **Instala.** Elige el juego, dónde instalarlo y pulsa **Instalar**. El juego queda en
   `<unidad>/homebrew/<TITLE_ID>`, todavía comprimido.
7. **Juega.** Si ShadowMountPlus está en marcha, el instalador le registra el juego y le fija el modo
   de fakelib. Kraken Mount (sin ShadowMountPlus en marcha) lo encuentra en `homebrew/`.

### Jugar desde el USB (sin instalador)

1. Haz los pasos 1 y 2 de arriba. No necesitas `maxpack.elf`. Si solo usas Kraken Mount, el paso 2
   tampoco hace falta, porque trae su propio ampr-emu-ng: cuando MaxPack lo pida, pulsa «Seguir sin
   él».
2. En **Comprimir**, en **Hecho para**, elige **Jugar desde el USB**. Como destino, elige la raíz del
   USB (por ejemplo `E:\`): cada juego va a `E:\homebrew\<TITLE_ID>`. Si eliges otra carpeta, cada
   juego va dentro de ella y luego tendrás que moverlo a `homebrew/`.
3. Conecta el USB a la PS5.
   - **Con ShadowMountPlus:** añade o vuelve a escanear el juego y deja su modo de fakelib en
     «Predeterminado (Completo)»: así ShadowMountPlus carga el `libSceAmpr.sprx` del juego y, si hace
     falta, añade su backport (aviso «Backport aplicado: <TITLE_ID>»). Comprobado en una PS5
     ([sección 8](#8-qué-está-probado-y-qué-falta)). Los pasos: [probar-en-ps5.md](probar-en-ps5.md).
   - **Con Kraken Mount:** no hay que hacer nada más.

## 3. Descargar e instalar el programa del PC

Cuando haya versiones publicadas, estarán en **Releases** del repositorio. Hasta entonces, GitHub
Actions construye todo en cada cambio y lo deja como artefactos:

| Artefacto | Qué lleva |
|---|---|
| `maxpack-windows-x64` | `maxpack-windows-x64-setup.exe` (el instalador) y `maxpack-windows-x64-portable.zip` (la versión portable) |
| `maxpack-macos-apple-silicon` | `MaxPack.app` en un `.dmg` y en un `.tar.gz`, para Mac con chip de Apple |
| `maxpack-macos-intel` | Lo mismo, para Mac con Intel |
| `maxpack-linux-x64` | `maxpack-linux-x64.tar.gz`, con `install.sh` |
| `maxpack-ps5` | `maxpack.elf`, el instalador de la PS5 |

Para bajarlos:
1. En https://github.com/DeLFuS77/maxpack abre **Actions**, el último trabajo `build` en verde y, abajo,
   **Artifacts**.
2. Hace falta tener sesión iniciada en GitHub, y los artefactos caducan pasado un tiempo.
3. Cada artefacto baja como un zip que envuelve el paquete de verdad: descomprímelo primero.

Después, según tu sistema:
- **Windows.** El instalador pone MaxPack en tu carpeta de usuario, sin permisos de administrador, con
  su acceso directo; se desinstala desde «Aplicaciones instaladas». La versión portable se descomprime
  donde quieras y guarda sus ajustes en su carpeta `settings`. `MaxPack.exe` y `maxpack-core.exe`
  tienen que estar juntos. Ni el instalador ni `MaxPack.exe` están firmados: la primera vez,
  SmartScreen avisará ([qué hacer](#smartscreen-y-gatekeeper)).
- **macOS.** Abre el `.dmg` y arrastra `MaxPack.app` a Aplicaciones. La app no está notarizada: la
  primera vez, ábrela con clic derecho, **Abrir** ([qué hacer](#smartscreen-y-gatekeeper)).
- **Linux.** Descomprime el `.tar.gz` y ejecuta `sh install.sh`. Pone MaxPack en el menú de
  aplicaciones y `maxpack` en `~/.local/bin`. `sh install.sh --uninstall` lo quita.

Solo hay una ventana: abrir MaxPack con juegos (arrastrándolos a su icono, por ejemplo) los añade a la
que ya está abierta. Los detalles de cada sistema están en
[ventana-pc.md](ventana-pc.md#1-descargar-y-abrir).

## 4. Comprimir, Descomprimir, Verificar y Detalles

La barra lateral de la ventana tiene cuatro secciones de trabajo y los Ajustes. Todo está explicado,
con capturas, en [ventana-pc.md](ventana-pc.md).

**Comprimir** ([detalles](ventana-pc.md#2-comprimir))
- Valen las carpetas de dump y las imágenes `.exfat`. **Añadir carpeta** acepta un juego o una
  carpeta con juegos justo debajo. Una carpeta ya comprimida se salta («ya está comprimido»).
- **Hecho para:** «Instalador MaxPack» (un `.p5z`) o «Jugar desde el USB» (una carpeta en
  `homebrew/`).
- **Nivel de compresión:** del 1 al 22, 19 por defecto, con los atajos **Ligero**, **Medio** y
  **Máximo**. Del 20 al 22 se usa mucha memoria y se tarda bastante más, para ahorrar poco más.
- Cada juego se verifica al terminar. Mientras hay trabajo, el PC no se duerme, y el progreso sale
  también en la barra de tareas (Windows) o en el Dock (macOS).
- Al final, MaxPack dice cuánto se ahorró, qué juegos ahorraron más y qué hacer ahora.
- **Cancelar** detiene el juego en marcha, borra lo que llevaba escrito y deja el destino como
  estaba; los juegos que esperaban no empiezan.

**Descomprimir** ([detalles](ventana-pc.md#3-descomprimir-y-verificar))
- Devuelve un `.p5z` al **dump original (bit a bit)**, con cada archivo comprobado con su SHA-256, o a
  la **carpeta para la PS5**, la misma que sale de «Jugar desde el USB».
- Si el juego era una imagen `.exfat`, el dump original sale como carpeta con sus archivos, no como
  imagen.

**Verificar** lee un `.p5z` o una carpeta comprimida y comprueba que siguen enteros. No cambia nada.

**Detalles** enseña lo que dice un juego: título, ID, versión, tamaños, archivos, packs y si trae un
runtime que lea zstd.

**Ajustes** ([detalles](ventana-pc.md#4-ajustes)): idioma (español o inglés), tema (sistema, claro u
oscuro) y el runtime de ampr-emu-ng para los juegos que no lo traen. MaxPack solo acepta un
`libSceAmpr.sprx` que lea zstd.

## 5. En la PS5

### El instalador MaxPack

`maxpack.elf` se carga como cualquier payload y sirve una página en el puerto 8095. Los detalles están
en [instalador-ps5.md](instalador-ps5.md).

- **Qué instala:**
  - los `.p5z`: copia los packs tal cual y solo descomprime los archivos pequeños que van sueltos;
  - tal cual, sin comprimir nada: carpetas (dumps, o carpetas con packs de Dump Forge) e imágenes
    `.exfat`, `.ffpkg`, `.ffpfs` y `.ffpfsc`. Una `.ffpkg`, `.ffpfs` o `.ffpfsc` tiene que llevar el
    ID del juego en el nombre, por ejemplo `Silksong-PPSA12544.ffpkg`.
- **Dónde busca:** en cada USB y en el almacenamiento ampliado (en la raíz y un nivel por debajo), y
  en `/data/maxpack/`. Si algo no aparece, escribe su ruta a mano.
- **Reemplazar:** si el juego ya está en la consola (aunque sea tu dump sin comprimir), te dice qué se
  borrará y cuánto ocupa, y pide confirmación. Lo viejo solo se borra cuando lo nuevo está instalado y
  verificado.
- **Pausar y seguir:** **Pausar** detiene la instalación sin perder lo hecho. Si se corta (USB
  quitado, reposo, apagón), el juego aparece en **Instalaciones a medias**, con **Seguir** y
  **Descartar**.
- **Borrar el `.p5z`:** la casilla «Borrar el .p5z cuando esté instalado y verificado» relee el juego
  entero antes de borrar el `.p5z`, porque después será la única copia.
- **Verificar y Desinstalar** los juegos instalados con MaxPack. Cierra el juego antes de
  desinstalarlo o reemplazarlo: ShadowMountPlus 1.7 no dice qué juego está abierto.
- La página solo acepta peticiones que llegan por la IP de la consola y desde la propia página.

### ShadowMountPlus o Kraken Mount

Los dos montan los juegos de `homebrew/`. Elige uno:

| | ShadowMountPlus | Kraken Mount |
|---|---|---|
| ampr-emu-ng | El juego tiene que llevarlo en `fakelib/` (MaxPack lo pone). Con el modo de fakelib predeterminado («Completo»), ShadowMountPlus usa el del juego. | Trae el suyo. |
| Con el instalador | Si está en marcha, el instalador registra el juego y le fija el modo de fakelib. Si no lo está, o aplaza el escaneo, el juego sale como «Pendiente en ShadowMountPlus» y el instalador lo reintenta cada minuto. | Explora las mismas rutas. No hay que hacer nada más. |
| Sin el instalador | Añade el juego y deja su modo de fakelib en «Predeterminado (Completo)». | Nada más que hacer. |

Kraken Mount se usa sin ShadowMountPlus en marcha. Más detalles:
[instalador-ps5.md](instalador-ps5.md#3-shadowmountplus-y-kraken-mount).

### Dónde tienen que estar los juegos

Cada juego va en su carpeta `homebrew/<TITLE_ID>`, en la raíz de la unidad:

| Unidad | En la PS5 | En el PC (ejemplo) |
|---|---|---|
| Almacenamiento interno | `/data/homebrew/<TITLE_ID>` | — |
| USB | `/mnt/usb0/homebrew/<TITLE_ID>` (o `usb1`…`usb7`) | `E:\homebrew\<TITLE_ID>` |
| Almacenamiento ampliado | `/mnt/ext0/homebrew/<TITLE_ID>` (o `ext1`) | — |

El instalador siempre los deja ahí, y «Jugar desde el USB» también, si eliges la raíz del USB como
destino. Si un juego está en otra carpeta, muévelo a `homebrew/`.

## 6. La línea de comandos

La ventana hace todo lo que la mayoría necesita. La línea de comandos sirve para guiones, para medir un
juego antes de comprimirlo (`analyze`, que la ventana no tiene) o si prefieres escribir comandos.

**Dónde está:**
- **Windows:** `MaxPack.exe` con argumentos hace de `maxpack`. Está en
  `%LOCALAPPDATA%\Programs\MaxPack` (con el instalador) o en la carpeta del zip portable.
  `maxpack-core.exe`, a su lado, también vale.
- **Linux:** después de `sh install.sh`, `maxpack` (en `~/.local/bin`).
- **macOS:** `/Applications/MaxPack.app/Contents/MacOS/maxpack`.
- **Compilándolo:** `maxpack`, como cuenta el [README](../README.md#compilar).

Los ejemplos usan `maxpack`. En Windows, escribe `MaxPack.exe` (o su ruta completa) en su lugar. Sin
argumentos, `MaxPack` abre la ventana; `maxpack --help` muestra el uso.

```
maxpack analyze <carpeta|imagen.exfat> [opciones]
maxpack pack <carpeta|imagen.exfat> -o <salida> [opciones]
maxpack unpack <juego.p5z> --folder|--original -o <carpeta> [--threads N]
maxpack info <juego.p5z>
maxpack info --json <carpeta|imagen.exfat|juego.p5z>
maxpack icon <carpeta|imagen.exfat|juego.p5z> -o <icono.png>
maxpack verify <juego.p5z | carpeta> [--against <origen>] [--threads N]
maxpack --version
maxpack --capabilities
```

Códigos de salida: 0 bien, 1 error, 2 uso incorrecto y 130 cancelado.

### `pack`: comprimir

```
maxpack pack "D:\dumps\MiJuego.exfat" -o "E:\homebrew\PPSA00000" --runtime "D:\ampr-emu-ng\libSceAmpr.sprx"
maxpack pack "D:\dumps\MiJuego.exfat" -o "D:\juegos\MiJuego [PPSA00000].p5z" --p5z --runtime "D:\ampr-emu-ng\libSceAmpr.sprx"
```

| Opción | Qué hace | Ejemplo |
|---|---|---|
| `-o SALIDA` | Dónde dejar el resultado: con `--folder`, una carpeta nueva o vacía; con `--p5z`, un archivo que no exista, en una carpeta que sí exista. | `-o "E:\homebrew\PPSA00000"` |
| `--folder` | Salida en carpeta, lista para la consola. Es lo que hace si no dices nada. | `--folder` |
| `--p5z` | Salida en un solo archivo `.p5z`. | `--p5z` |
| `--level N` | Nivel de zstd, de 1 a 22 (19 por defecto). | `--level 22` |
| `--threads N` | Hilos (por defecto, todos los de la CPU). El resultado es el mismo con cualquier número. | `--threads 8` |
| `--runtime ARCHIVO` | El `libSceAmpr.sprx` de ampr-emu-ng, que se pone en `fakelib/`. | `--runtime "D:\ampr-emu-ng\libSceAmpr.sprx"` |
| `--keep-loose GLOB` | Deja suelto también lo que case. Se puede repetir. `*` y `?` valen dentro de una carpeta; `**`, para cualquier número de carpetas. | `--keep-loose "**/*.bnk"` |
| `--max-file-size N` | Solo con `--p5z`: si el `.p5z` va a pasar de N bytes, para y lo borra. | `--max-file-size 4294967295` (FAT32) |
| `--progress json` | Una línea JSON por evento en stdout; el texto para personas sigue en stderr. | `--progress json` |
| `--control stdin` | Lee la entrada: la línea `cancel`, o cerrar la entrada, cancela y borra lo escrito. | `--control stdin` |

Lo que garantiza:
- Nunca escribe en el origen, y la salida no puede ser el juego ni estar dentro de él.
- No acepta una salida que no esté vacía, ni un `.p5z` que ya exista.
- Se niega con juegos ya empaquetados y con rutas que solo se diferencian en mayúsculas.
- Un empaquetado cortado no se monta, y un `.p5z` cortado o dañado se detecta al abrirlo.
- Con las mismas opciones, el resultado es idéntico byte a byte.

Avisos que puede dar al terminar:
- `aviso: el runtime de fakelib/libSceAmpr.sprx no parece leer packs zstd: usa el libSceAmpr.sprx de ampr-emu-ng (--runtime)`
- `aviso: sce_sys/param.json no da el TITLE_ID del juego (titleId): el instalador de MaxPack no aceptará este .p5z`

Con `--progress json`, las líneas son así:

```
{"event":"progress","phase":"packing","done":3137495,"total":3137495}
{"event":"result","ok":true,"original_size":3147695,"output_size":2106626,"loose_bytes":10200,"packed_bytes":3137495,"stored_bytes":2066945,"volumes":1,"runtime_without_zstd":true}
```

`--control stdin` está pensado para una interfaz. A mano, escribe `cancel` y pulsa Enter. Si la entrada
ya está cerrada (por ejemplo, con `< /dev/null`), cancela en el acto.

### `unpack`: descomprimir un `.p5z`

| Opción | Qué hace | Ejemplo |
|---|---|---|
| `--folder` | Saca la carpeta para la consola, igual que `pack --folder`. | `maxpack unpack "D:\juegos\MiJuego.p5z" --folder -o "E:\homebrew\PPSA00000"` |
| `--original` | Reconstruye el dump original bit a bit y comprueba cada SHA-256. | `maxpack unpack "D:\juegos\MiJuego.p5z" --original -o "D:\dumps\MiJuego"` |
| `-o CARPETA` | Una carpeta nueva o vacía. | `-o "D:\dumps\MiJuego"` |
| `--threads N` | Hilos. | `--threads 8` |
| `--progress json`, `--control stdin` | Como en `pack`. | `--progress json` |

### `verify`: comprobar

| Uso | Qué hace | Ejemplo |
|---|---|---|
| Con un `.p5z` | Comprueba cada archivo con su SHA-256. | `maxpack verify "D:\juegos\MiJuego.p5z"` |
| Con una carpeta | Comprueba los packs y los índices. | `maxpack verify "E:\homebrew\PPSA00000"` |
| `--against ORIGEN` | Solo con carpetas: la compara además con el juego original. | `maxpack verify "E:\homebrew\PPSA00000" --against "D:\dumps\MiJuego.exfat"` |
| `--threads N` | Hilos. | `--threads 8` |
| `--progress json`, `--control stdin` | Como en `pack`. | `--control stdin` |

Al acabar dice, por ejemplo, `verify: 216 correctos, 0 con fallos (0 s)`. Si algún archivo falla, sale
con el código 1.

### `info` e `icon`: lo que dice un juego

| Orden | Qué hace | Ejemplo |
|---|---|---|
| `info` | Título, ID, versión, archivos, packs y tamaño de un `.p5z`, para personas. | `maxpack info "D:\juegos\MiJuego.p5z"` |
| `info --json` | Lo mismo de un `.p5z`, una carpeta o un `.exfat`, en una línea JSON. Es lo que lee la ventana. | `maxpack info --json "D:\dumps\MiJuego.exfat"` |
| `icon` | Guarda el `sce_sys/icon0.png` del juego. Sale con el código 1 si no tiene. | `maxpack icon "D:\dumps\MiJuego.exfat" -o icono.png` |

La línea de `info --json` lleva `kind` (`folder`, `exfat` o `p5z`), `title_id`, `version`, `name`,
`original_size`, `install_size`, `p5z_size`, `files`, `packed_files`, `volumes`, `has_runtime`,
`has_zstd_runtime` y `packed`.

### `analyze`: medir antes de comprimir

Mide cuánto ahorraría cada forma de comprimir un juego, sin escribir nada:

```
maxpack analyze "D:\dumps\MiJuego.exfat" --json informe.json
```

| Opción | Qué hace | Ejemplo |
|---|---|---|
| `--sample N%` | Qué parte del juego se comprime para medir (por defecto el 2 %, al menos 1 GiB). | `--sample 5%` |
| `--full` | Mide todo el juego, sin muestra. Tarda más, pero es exacto. | `--full` |
| `--json ARCHIVO` | Guarda también el informe en JSON. No puede ir dentro del juego. | `--json informe.json` |
| `--threads N` | Hilos (por defecto, todos los de la CPU). | `--threads 8` |
| `--keep-loose GLOB` | Como en `pack`. | `--keep-loose "**/*.bnk"` |
| `--no-dedup` | No hace el pase de deduplicación, que lee todo el juego. | `--no-dedup` |
| `--no-transport` | No mide el `.p5z` de transporte (zstd 19, ventana larga). | `--no-transport` |
| `--list` | Solo lista los archivos y si irían sueltos o empaquetados, sin medir. | `maxpack analyze "D:\dumps\MiJuego.exfat" --list` |

El progreso sale por stderr y el informe por stdout. Cómo leerlo:
[README](../README.md#cómo-leer-el-informe).

### Otras

| Orden | Qué hace | Ejemplo |
|---|---|---|
| `--version` | La versión del motor y de zstd y zlib. | `maxpack --version` → `maxpack 0.0.1 (zstd 1.5.6, zlib 1.3.1)` |
| `--capabilities` | Lo que sabe hacer esta versión, en JSON. La ventana lo usa para saber si su `maxpack-core` le vale. | `maxpack --capabilities` |
| `--help` | El uso, con todas las opciones (también `-h` o `help`). | `maxpack --help` |

Para probar el instalador sin consola, los que compilan MaxPack tienen `maxpack-installer-pc`:
[instalador-ps5.md](instalador-ps5.md#compilar-maxpackelf).

## 7. Problemas frecuentes

Las frases entre comillas son las que ves en pantalla: en la ventana, en la línea de comandos, en la
página del instalador o en la consola.

### FAT32 y los 4 GiB

Lo que ves en la ventana:
- «El destino es FAT32», antes de empezar, si el destino es FAT32 y el juego original pasa de 4 GiB.
- «el .p5z llegó a 4 GiB, que no cabe en FAT32: se paró y se borró», si seguiste y no cupo.

Qué hacer:
- **Usar carpeta** cambia a «Jugar desde el USB», cuyas partes caben en FAT32.
- **Seguir igualmente** lo intenta. Si el `.p5z` va a pasar de 4 GiB menos un byte, MaxPack para y
  borra lo escrito.
- Lo mejor: un USB en exFAT, que no tiene ese límite. Un `.p5z` grande guardado en el disco del PC
  tampoco se puede copiar luego a un USB en FAT32.

En la línea de comandos, `--max-file-size 4294967295` hace lo mismo que **Seguir igualmente**.

### No hay espacio

Lo que ves:
- «puede no caber en el destino», antes de empezar: el destino tiene menos libre de lo que ocupa el
  juego original. El resultado suele ocupar menos, así que puedes pulsar **Empezar igualmente**.
- «no caben lo nuevo y lo de ahora a la vez: elige otro destino o haz sitio», con **Reemplazar**: ese
  juego no empieza. Puedes elegir otro destino o seguir sin él.
- En la PS5, «No cabe: faltan …». El instalador no empieza.

Qué hacer: haz sitio o elige otro destino.

### El juego ya está en el destino (Reemplazar)

Lo que ves: «Ya está en el destino», con lo que se borraría y cuánto ocupa, y tres botones:
**Cancelar**, **Saltar** y **Reemplazar**.

Qué pasa si eliges **Reemplazar**: lo nuevo se escribe aparte, en `<destino>.maxpack-tmp`, y se
verifica; solo entonces se borra lo de antes. Por eso tienen que caber los dos a la vez. Si cancelas o
algo falla (por ejemplo, quitas el USB), lo de antes sigue intacto.

En la PS5 pasa lo mismo: el instalador dice qué se borrará y pregunta «¿Instalar y reemplazar?».

### El runtime no lee zstd (falta ampr-emu-ng)

Lo que ves:
- En la ventana, antes de empezar: «Falta ampr-emu-ng». En la lista: «no trae un ampr-emu-ng que lea
  zstd: no arrancará hasta ponérselo».
- Al terminar: «hecho, pero su libSceAmpr.sprx no lee zstd: no arrancará hasta ponerle el de
  ampr-emu-ng».
- En Ajustes, si eliges otro archivo: «Ese libSceAmpr.sprx no lee zstd: no es el de ampr-emu-ng. Elige
  otro.»
- En la línea de comandos: `aviso: el runtime de fakelib/libSceAmpr.sprx no parece leer packs zstd`.
- En la PS5: «Este .p5z no trae ampr-emu-ng y el instalador no lleva ninguno. Vuelve a empaquetarlo
  con --runtime.»

Qué hacer:
1. Descarga `libSceAmpr.sprx` de las Releases de la
   [copia de ampr-emu-ng de MaxPack](https://github.com/DeLFuS77/ampr-emu-ng) y elígelo en **Ajustes**.
2. Vuelve a comprimir el juego. En la ventana, «empaquetar con `--runtime`» es esto mismo: comprimir con
   el runtime elegido en Ajustes.
3. Si ya no tienes el dump, **Descomprimir** el `.p5z` a «Dump original (bit a bit)» lo recupera, y
   luego lo comprimes otra vez.

### Un `.p5z` sin TITLE_ID

Lo que ves:
- En la ventana, antes de empezar con «Instalador MaxPack»: «su param.json no da el ID del juego
  (TITLE_ID): el instalador MaxPack no aceptará el .p5z».
- En la línea de comandos: `aviso: sce_sys/param.json no da el TITLE_ID del juego (titleId)`.

Qué hacer: el instalador de la PS5 no acepta un `.p5z` así. Comprueba que el dump está completo y que
su `sce_sys/param.json` trae el `titleId`. Un `param.json` con BOM o con relleno al final se lee bien.

### «maxpack-core de otra versión» o «falta maxpack-core»

Lo que ves: en la ventana, «maxpack-core, el motor de MaxPack, es de otra versión: no hace todo lo que
la ventana necesita…» o «No se encuentra maxpack-core, el motor de MaxPack». No deja empezar ningún
trabajo.

Qué hacer: pon junto a MaxPack el `maxpack-core` que venía con ella. Lo más fácil es volver a instalar
MaxPack o descomprimir otra vez el zip portable entero.

### SmartScreen y Gatekeeper

- **Windows:** ni el instalador ni `MaxPack.exe` están firmados, así que SmartScreen avisa la primera
  vez («Windows protegió su PC»). Pulsa **Más información** y **Ejecutar de todas formas**.
- **macOS:** la app no está notarizada. La primera vez, ábrela con clic derecho, **Abrir**. En macOS
  15 o posterior, si no te deja, intenta abrirla una vez y luego, en Ajustes del Sistema, Privacidad y
  seguridad, pulsa **Abrir igualmente**.

### El juego no aparece en ShadowMountPlus

1. **¿Está en `homebrew/`?** Si lo comprimiste para «Jugar desde el USB» en otra carpeta, MaxPack te
   lo dice al terminar: «ShadowMountPlus solo los ve en homebrew/». Muévelo a `homebrew/<TITLE_ID>`
   del USB o del almacenamiento interno.
2. **¿Lo instalaste con el instalador?** Mira la página:
   - «ShadowMountPlus no está en marcha: los juegos se registrarán cuando arranque»: carga
     ShadowMountPlus.
   - «Pendiente en ShadowMountPlus: …»: ShadowMountPlus aplazó el escaneo (por ejemplo, porque hay un
     juego abierto). El instalador lo reintenta cada minuto. Para hacerlo a mano, añade la ruta del
     juego en ShadowMountPlus.
3. **¿Usas Kraken Mount?** Entonces ShadowMountPlus no debe estar en marcha.

Si aparece pero **no arranca**:
- Lo más probable es el runtime: mira [la sección del runtime](#el-runtime-no-lee-zstd-falta-ampr-emu-ng)
  y que el modo de fakelib del juego en ShadowMountPlus sea «Predeterminado (Completo)». Si ampr-emu-ng funciona, al arrancar el
  juego debería salir su aviso «ampr-emu-ng: v… (zstd) running».
- Un archivo que el juego abre con `mmap` no puede ir empaquetado. Se deja suelto volviendo a
  empaquetar con `--keep-loose "<ruta o patrón>"` (solo en la línea de comandos).
- Qué anotar si falla: [probar-en-ps5.md](probar-en-ps5.md#4-qué-comprobar-diseño-68).

### El instalador de la PS5 no encuentra el juego o no abre

- «No hay ningún .p5z en los USB ni en /data/maxpack…»: pon el `.p5z` en la raíz del USB o en una
  carpeta justo debajo, y pulsa **Buscar otra vez**. También puedes escribir su ruta a mano.
- «MaxPack ya está en marcha»: ya estaba cargado. Abre la página directamente.
- «No hay conexión con la consola. Comprueba que MaxPack sigue cargado.»: vuelve a enviar
  `maxpack.elf`. Lo que se cortó aparece en **Instalaciones a medias**, para seguir donde iba.
- «El .p5z está dañado o no es válido.»: compruébalo en el PC con **Verificar** y, si falla, vuelve a
  comprimir el juego.

### Cancelar

**En la ventana**, **Cancelar** (o cerrar la ventana con trabajo en marcha) detiene el juego actual
sin dejar nada a medias ni sin verificar:
- lo que llevaba escrito se borra y el destino queda como estaba; con **Reemplazar**, lo de antes
  sigue intacto;
- los juegos que esperaban no empiezan, y los ya hechos se quedan;
- puede tardar unos segundos: `maxpack-core` acaba lo que tiene entre manos y luego borra. Si cierras
  la ventana, sigue solo hasta haberlo borrado;
- si algo no se puede borrar (por ejemplo, un archivo abierto en otro programa), la fila lo dice, con
  la ruta, para que lo borres tú: «no se pudo borrar lo escrito a medias».

**En la PS5**, **Pausar** detiene la instalación sin perder lo hecho. En **Instalaciones a medias**,
**Seguir** continúa donde se quedó y **Descartar** borra lo copiado.

**En la línea de comandos**, `--control stdin` permite cancelar (con la línea `cancel`). El motor
borra lo que estaba escribiendo y sale con el código 130.

### Otros avisos de la ventana

- «el destino es el propio juego, o está dentro de él: elige otro destino».
- «otro juego de la lista va al mismo sitio»: dos juegos con el mismo ID hacia el mismo destino.
- «el destino ya no está (¿se quitó el USB?)».
- «lo nuevo no pasó la verificación y se borró»: vuelve a intentarlo y, si se repite, prueba con otro
  destino.

La lista completa de avisos de la ventana está en [ventana-pc.md](ventana-pc.md#5-avisos).

## 8. Qué está probado y qué falta

**Probado en el PC, con juegos reales**, en Windows 11 y con la línea de comandos
([informe](informes/fase1.md)):
- Silksong: empaquetar, verificar y reconstruir el dump original, con cero fallos. El
  `ampr_emu.index` que genera MaxPack salió idéntico byte a byte al que trae el dump.
- RESIDENT EVIL requiem (133 GB, con archivos de hasta 72 GB): empaquetar en `.p5z` y verificar, con
  cero fallos.

**Probado en cada cambio, en GitHub Actions** ([el flujo](../.github/workflows/build.yml)):
- **Windows y Linux:** se compila el motor y pasan todas sus pruebas (`ctest`): imágenes exFAT
  sintéticas con casos difíciles, empaquetar y reconstruir, `.p5z` dañados, cancelar, y el motor y la
  página del instalador contra un ShadowMountPlus falso.
- **Windows, Linux y macOS (Apple silicon e Intel):** se compilan el motor y la ventana, pasan las
  pruebas de la ventana (Go) con ese motor y se hacen los paquetes.
- **Los paquetes, en automático:**
  - Windows: el instalador se instala en silencio, comprime una imagen de prueba y se desinstala;
  - Linux: `install.sh` instala, comprime y desinstala;
  - macOS: el `.dmg` se abre, la app copiada conserva su firma y comprime una imagen de prueba.
- **PS5:** `maxpack.elf` se compila con el SDK de payloads. No se ejecuta.
- En macOS no se pasan las pruebas del motor (`ctest`): allí el motor solo se prueba a través de las
  de la ventana.

**Probado en una PS5** (firmware 13.60, ShadowMountPlus 1.7beta12, 24/11/2026):
- Silksong empaquetado por MaxPack, en carpeta, arranca con ampr-emu-ng
  («ampr-emu-ng: v0.5.0… (zstd) running»), carga, guarda y vuelve a cargar partida
  ([probar-en-ps5.md](probar-en-ps5.md)).
- ShadowMountPlus, con el modo de fakelib predeterminado («Completo»), carga el `libSceAmpr.sprx` del
  juego y añade su backport.

**Sin probar todavía:**
- El instalador en una PS5 real
  ([lista de comprobación](instalador-ps5.md#6-lista-de-comprobación-en-consola)).
- El instalador de Windows con doble clic y el asistente en pantalla. Si algo no va, usa el zip
  portable y cuéntalo.

**Pendiente de comprobar en la consola**
([detalles](instalador-ps5.md#7-pendiente-de-comprobar-en-consola)):
- **P1. El modo de fakelib de ShadowMountPlus.** Comprobado: con `full` (el «Predeterminado
  (Completo)» de ShadowMountPlus 1.7) el juego carga su ampr-emu-ng, así que el instalador sigue
  usando `full`.
- **P2. Desinstalar.** Que, después de desinstalar, el juego desaparezca de ShadowMountPlus.
- **P3. Modo reposo.** Si el instalador sigue cargado después del reposo. Si no, se vuelve a cargar y
  la instalación sigue donde iba.
- **P4. El runtime del instalador.** Qué copia y qué versión de ampr-emu-ng llevará dentro
  `maxpack.elf`. Hoy no lleva ninguna, y por eso solo instala `.p5z` que ya la traen.
- **P5. USB sin conectar.** Si `/mnt/usb0` existe como carpeta vacía sin ningún USB. Entonces la
  página lo ofrecería como destino aunque no haya nada conectado.

## 9. Licencias y créditos

**MaxPack** es software libre con la licencia GPL-3.0-or-later ([LICENSE](../LICENSE)), de DeLFuS77, y
no tiene ninguna garantía. La ventana parte de [NXPress](https://github.com/DeLFuS77/NXPress), del
mismo autor; lo que viene de NXPress se publica aquí con la licencia de MaxPack.

**Lo que va dentro de los programas:**
- [zstd](https://github.com/facebook/zstd) 1.5.6 (Meta): BSD o GPLv2, a elegir. Va como submódulo en
  `third_party/zstd` y se compila dentro de `maxpack`, `maxpack-core` y `maxpack.elf`.
- [zlib](https://github.com/madler/zlib) 1.3.1 (Jean-loup Gailly y Mark Adler): licencia zlib. Va como
  submódulo en `third_party/zlib`, igual que zstd.
- La ventana, en Go: [Wails](https://github.com/wailsapp/wails) v2 (MIT); en Windows,
  go-webview2 (MIT), go-ole (MIT) y go-toast (MIT o Unlicense); en Linux, godbus (BSD);
  golang.org/x/net, x/sys y x/text (BSD); y otras librerías pequeñas con licencias MIT, BSD o
  Apache-2.0. La lista con versiones está en [gui/go.mod](../gui/go.mod).
- La página de la ventana se ve con el navegador web del sistema (WebView2 en Windows, WebKitGTK en
  Linux y WebKit en macOS), que no va dentro de MaxPack.
- `maxpack.elf` se compila con el SDK [ps5-payload-dev/sdk](https://github.com/ps5-payload-dev/sdk)
  (GPL-3.0).

**Proyectos con los que funciona MaxPack** (no van dentro):
- [ampr-emu-ng](https://github.com/quer3q/ampr-emu-ng), de quer3q (GPL-3.0): el runtime que lee los
  packs zstd en la consola. MaxPack usa su propia copia,
  [DeLFuS77/ampr-emu-ng](https://github.com/DeLFuS77/ampr-emu-ng), derivada de la v0.5.0 de quer3q.
  Lo descargas tú. Si compilas `maxpack.elf` con un ampr-emu-ng dentro, ese `libSceAmpr.sprx` sigue
  con su licencia GPL-3.0.
- [ampr_emu](https://github.com/drakmor/ampr_emu), de drakmor (GPL-3.0): de él viene el formato del
  índice `ampr_emu.index`, que MaxPack genera para cada juego.
- [ShadowMountPlus](https://github.com/drakmor/ShadowMountPlus), de drakmor (GPL-3.0).
- [Kraken Mount](https://github.com/quer3q/kraken-mount), de quer3q (GPL-3.0-or-later).
- [PS5 Dump Forge](https://github.com/quer3q/ps5-dump-forge), de quer3q (GPL-3.0): escribe los mismos
  packs zstd; MaxPack sigue sus reglas.
- [ps5upload](https://github.com/phantomptr/ps5upload), de phantomptr (GPL-3.0), otra herramienta de la
  escena.

## Más documentación

- [ventana-pc.md](ventana-pc.md): la ventana del PC, con capturas.
- [instalador-ps5.md](instalador-ps5.md): el instalador de la PS5 y lo pendiente en consola.
- [probar-en-ps5.md](probar-en-ps5.md): probar a mano en la consola un juego empaquetado.
- [formato-p5z.md](formato-p5z.md): el formato del `.p5z`.
- [research-notes.md](research-notes.md): lo investigado sobre la escena y los formatos.
- [README](../README.md): compilar MaxPack y leer el informe de `analyze`.
