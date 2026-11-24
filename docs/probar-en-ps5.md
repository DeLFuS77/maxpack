# Probar un juego empaquetado en la PS5

Esta guía es para comprobar en tu consola un juego empaquetado con `maxpack pack`. Necesitas:
- una PS5 con jailbreak y la pila habitual: cargador de ELF, kstuff y ShadowMountPlus;
- un dump tuyo del juego (carpeta o `.exfat`).

> Trabaja siempre sobre una copia: guarda el `.p5z` (o el dump original) antes de borrar nada.

## 1. El runtime

Los packs zstd los sirve **ampr-emu-ng**. Usa la que lleva MaxPack, no la de las releases de quer3q:
- Su `libSceAmpr.sprx` está en `vendor/ampr-emu-ng/libSceAmpr.sprx` del repositorio de la consola,
  [DeLFuS77/maxpack-ps5](https://github.com/DeLFuS77/maxpack-ps5).
- Se compila en [DeLFuS77/ampr-emu-ng](https://github.com/DeLFuS77/ampr-emu-ng) (el artefacto `sprx`
  de su GitHub Actions). `vendor/ampr-emu-ng/SOURCE` dice de qué commit sale.
- El `libSceAmpr.sprx` que traen algunos dumps es el ampr_emu antiguo y no lee estos packs.
  `maxpack pack` lo avisa: "el runtime ... no parece leer packs zstd".

## 2. Empaquetar

```
maxpack pack "D:\dumps\MiJuego.exfat" -o "D:\paraPS5\PPSA00000" --runtime "D:\ampr-emu-ng\libSceAmpr.sprx"
maxpack verify "D:\paraPS5\PPSA00000" --against "D:\dumps\MiJuego.exfat"
```

Para guardar el juego en el PC ocupando poco, en lugar de la carpeta:

```
maxpack pack "D:\dumps\MiJuego.exfat" -o "D:\juegos\MiJuego.p5z" --p5z --runtime "D:\ampr-emu-ng\libSceAmpr.sprx"
maxpack unpack "D:\juegos\MiJuego.p5z" --folder -o "D:\paraPS5\PPSA00000"
```

## 3. Llevarlo a la consola

1. Copia la carpeta entera a una ruta que ShadowMountPlus explore. Por defecto lo son
   `/data/homebrew/<TITLE_ID>` en el almacenamiento interno, o `homebrew/` en un USB
   (`/mnt/usb0..7`) o en `/mnt/ext0..1`.
   - Si la copias por FTP con FileZilla, ponlo en modo **binario** (Transferencia > Tipo de
     transferencia > Binario). Si no, los archivos sin extensión, como `sce_sys/keystone`, fallan con
     «504 STOR in ASCII mode is not supported».
2. En ShadowMountPlus, añade o vuelve a escanear el juego. Deja su modo por juego como viene,
   «Predeterminado (Completo)»: no hace falta desactivar la actualización de fakelib.
   - ShadowMountPlus usa el `fakelib/libSceAmpr.sprx` del juego (ampr-emu-ng) y solo le añade su
     backport, y lo dice con el aviso «Backport aplicado: <TITLE_ID>».
   - Comprobado el 24/11/2026 con ShadowMountPlus 1.7beta12 en una PS5 con firmware 13.60.

## 4. Qué comprobar (diseño 6.8)

- Al arrancar sale el aviso «ampr-emu-ng: v… (zstd) running»: el juego usa ampr-emu-ng.
- Arranca hasta el menú.
- Los vídeos se reproducen.
- Una partida carga.
- Guardar funciona.
- 10 minutos de juego sin tirones raros ni cuelgues.

Si algo falla, anota qué juego era, qué hacía en ese momento y, si tienes la versión de depuración
de ampr-emu-ng, su `/fsfail`. Un archivo que el juego abre con `mmap` no puede ir empaquetado: se
deja suelto volviendo a empaquetar con `--keep-loose "<ruta o patrón>"`.
