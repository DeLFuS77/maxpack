# El instalador en la PS5 (`maxpack.elf`)

El instalador deja tus juegos listos para jugar en la consola. Instala los `.p5z` que hace
`maxpack pack`, que siguen comprimidos en la consola. También instala tal cual juegos en los formatos
de siempre: carpetas (dumps, o carpetas con packs de Dump Forge) e imágenes `.exfat`, `.ffpkg`,
`.ffpfs` y `.ffpfsc`. En la consola nunca se comprime nada.

Necesitas una PS5 con jailbreak, un cargador de ELF (elfldr, etaHEN o Payload Manager) y, para
jugar, ShadowMountPlus o Kraken Mount.

> Antes de nada, comprueba en tu consola que un juego empaquetado con `maxpack pack` se juega bien:
> [probar-en-ps5.md](probar-en-ps5.md). El instalador solo coloca esa misma carpeta.

## 1. Cargar el instalador

1. Descarga `maxpack.elf`. Lo genera GitHub Actions en cada cambio, como artefacto `maxpack-ps5`.
2. Envíalo al cargador de ELF, en el puerto 9021. Por ejemplo, desde Linux o macOS:
   `nc -q0 <IP de la PS5> 9021 < maxpack.elf`.
3. La consola muestra el aviso "MaxPack … listo".
4. Abre `http://<IP de la PS5>:8095` en el navegador de la PS5, del móvil o del PC, dentro de tu red.

Si sale "MaxPack ya está en marcha", es que ya estaba cargado: abre la página directamente.

La página solo acepta peticiones que llegan por la IP de la consola y desde la propia página: otra web
abierta en un móvil o un PC de tu red no puede instalar ni borrar nada.

## 2. Instalar

1. Conecta un USB con tus juegos. Si un `.p5z` pesa más de 4 GiB, el USB tiene que estar en exFAT.
   El instalador busca en la raíz de cada USB, en la del almacenamiento ampliado y un nivel por
   debajo, y también en `/data/maxpack/`. Si algo no aparece, escribe su ruta a mano.
2. Elige el juego, elige dónde instalarlo y pulsa **Instalar**.
   - Un `.p5z` se instala en `<unidad>/homebrew/<TITLE_ID>`. Se copian los packs tal cual y solo
     se descomprimen los archivos pequeños que el sistema necesita sueltos.
   - Una carpeta va a `<unidad>/homebrew/<TITLE_ID>` y una imagen a `<unidad>/homebrew/<nombre>`,
     idénticas al original. El original no se borra.
   - Una imagen `.ffpkg`, `.ffpfs` o `.ffpfsc` tiene que llevar el ID del juego en el nombre, por
     ejemplo `Silksong-PPSA12544.ffpkg`.
3. Si el juego ya está en la consola (aunque sea tu dump sin comprimir, o en otra unidad), el
   instalador te dice qué se borrará y cuánto ocupa, y te pide confirmación. Lo viejo solo se borra
   cuando lo nuevo ya está instalado y verificado.
4. **Pausar** detiene la instalación sin perder lo hecho. Si se corta (USB quitado, reposo, apagón),
   aparece en **Instalaciones a medias**:
   - **Seguir** continúa donde se quedó y relee lo ya copiado;
   - **Descartar** borra lo copiado hasta entonces.

   Lo que ya ocupa la copia a medias cuenta como espacio disponible al seguir. Si un corte pilla al
   instalador justo cuando reemplaza un juego, al volver a cargarlo deja el juego viejo en su sitio.
5. "Borrar el .p5z cuando esté instalado y verificado" relee el juego instalado entero antes de borrar
   el `.p5z`, porque después será la única copia.

El progreso y los avisos también salen en la consola. Mientras se instala no hace falta tener la
página abierta.

## 3. ShadowMountPlus y Kraken Mount

- **ShadowMountPlus:** si está en marcha, el instalador registra el juego (`manual/add` y `scan`).
  A los juegos con packs zstd les fija además el modo de fakelib, para que ShadowMountPlus no cambie
  ampr-emu-ng por su `ampr_emu` antiguo. Si ShadowMountPlus no está en marcha o aplaza el escaneo (por
  ejemplo, porque hay un juego abierto), el juego sale como **Pendiente en ShadowMountPlus** y el
  instalador lo reintenta solo cada minuto. Para hacerlo a mano: añade la ruta del juego en
  ShadowMountPlus y, en su perfil, usa el modo de fakelib «Completo» (`full`). El que viene,
  «Predeterminado (Completo)», ya vale (ver P1).
- **Kraken Mount:** explora las mismas rutas (`/data/homebrew`, `homebrew/` de cada unidad) y trae su
  propio ampr-emu-ng. No hay que hacer nada más.

## 4. Verificar y desinstalar

- **Verificar** relee el juego instalado:
  - en uno de un `.p5z`, comprueba los packs y sus bloques y, si el `.p5z` sigue donde estaba,
    compara cada archivo con él;
  - en uno instalado tal cual, compara cada archivo con el SHA-256 guardado al copiarlo.
- **Desinstalar** solo funciona con juegos instalados con MaxPack. Pide confirmación, borra la
  carpeta o la imagen y quita la ruta de ShadowMountPlus.
- ShadowMountPlus 1.7 no dice qué juego está abierto, así que **cierra el juego antes de
  desinstalarlo o reemplazarlo**.

## 5. Ajustes

`/data/maxpack/config.json`:

```json
{ "smp_fakelib_mode": "full" }
```

Los valores posibles son `full`, `emulators`, `disabled` y `default`. El valor por defecto es `full`,
y está comprobado en consola (ver P1): no hace falta cambiarlo.

Lo que guarda el instalador, siempre fuera de las carpetas de los juegos:
- `/data/maxpack/installed/`: el registro de lo instalado;
- `/data/maxpack/state/`: las instalaciones a medias;
- `/data/maxpack/tmp/` y `<unidad>/maxpack-tmp/`: las carpetas temporales.

## 6. Lista de comprobación en consola

- [ ] Cargar `maxpack.elf` y abrir la página desde la PS5 y desde el móvil.
- [ ] Instalar Silksong (`.p5z`) del USB al interno. Quitar el USB a mitad, volver a ponerlo y
      reanudar con Instalar.
- [ ] Que ShadowMountPlus lo registre y se juegue: arranque, vídeos, cargar partida, guardar y 10
      minutos de juego. Lo mismo con Kraken Mount (sin ShadowMountPlus en marcha).
- [ ] Instalar un dump en carpeta y una imagen `.ffpkg` tal cual, y que se jueguen.
- [ ] Reemplazar el dump sin comprimir de un juego por su `.p5z` (con confirmación).
- [ ] Verificar y desinstalar.
- [ ] Instalar RE Requiem (79 GB) y anotar cuánto tarda.

## 7. Pendiente de comprobar en consola

- **P1. Modo de fakelib de ShadowMountPlus. Respondida: `full`.**
  - Comprobado el 24/11/2026 en una PS5 con firmware 13.60 y ShadowMountPlus 1.7beta12, con
    Silksong empaquetado por MaxPack.
  - Con `full` («Completo»; el modo por juego que viene es «Predeterminado (Completo)»),
    ShadowMountPlus usa el `fakelib/libSceAmpr.sprx` del juego y solo le añade su backport (aviso
    «Backport aplicado: <TITLE_ID>»). Sale «ampr-emu-ng: v… (zstd) running», y el juego carga y
    guarda partida.
  - Como el modo que viene ya es ese, el primer escaneo, antes de que el instalador fije el modo,
    tampoco le cambia el runtime.
  - No hizo falta probar `emulators` ni `disabled`.
- **P2. Desinstalar.** Comprueba que, tras desinstalar, el juego desaparece de ShadowMountPlus.
  El instalador llama a `manual/remove` y a `scan`.
- **P3. Modo reposo.** Comprueba si el instalador sigue cargado después del reposo. Si no, vuelve a
  cargarlo: la instalación sigue donde iba.
- **P5. USB sin conectar.** Sin ningún USB, comprueba si `/mnt/usb0` sigue existiendo como carpeta vacía.
  Si es así, la página lo ofrecerá como destino aunque no haya nada conectado. Avísame para que el
  instalador lo distinga.
- **P4. El runtime.** Hay que decidir qué copia de ampr-emu-ng lleva el instalador dentro: la de Dump
  Forge o la de Kraken Mount, y qué versión. Mientras tanto se compila sin runtime dentro, y solo se
  instalan `.p5z` empaquetados con `--runtime` y carpetas que ya traen ampr-emu-ng.

## Compilar `maxpack.elf`

Con el SDK [ps5-payload-dev/sdk](https://github.com/ps5-payload-dev/sdk) v0.45 en Linux o macOS:

```
/opt/ps5-payload-sdk/bin/prospero-cmake -B build-ps5 -DMAXPACK_PS5=ON -DCMAKE_BUILD_TYPE=Release
cmake --build build-ps5
```

Con `-DMAXPACK_RUNTIME=<libSceAmpr.sprx>` (y `-DMAXPACK_RUNTIME_VERSION=<texto>`), el instalador lleva
dentro ese ampr-emu-ng. El flujo `.github/workflows/build.yml` hace todo esto en cada cambio.

Para probar sin consola, `maxpack-installer-pc` ejecuta el mismo motor y la misma página sobre
carpetas del PC:

```
maxpack-installer-pc --internal <carpeta> --drive <carpeta-usb> --runtime <libSceAmpr.sprx>
```

Después se abre `http://127.0.0.1:8095`.
