# Probar un juego empaquetado en la PS5

Esta guía es para comprobar en tu consola un juego empaquetado con `maxpack pack`. Necesitas:
- una PS5 con jailbreak y la pila habitual: cargador de ELF, kstuff y ShadowMountPlus;
- un dump tuyo del juego (carpeta o `.exfat`).

> Trabaja siempre sobre una copia: guarda el `.p5z` (o el dump original) antes de borrar nada.

## 1. El runtime

Los packs zstd los sirve **ampr-emu-ng**:
- Descarga su `libSceAmpr.sprx` de las releases del proyecto (https://github.com/quer3q/ampr-emu-ng).
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
2. En ShadowMountPlus, añade o vuelve a escanear el juego y **desactiva la actualización de
   fakelib para ese juego**. Si no, ShadowMountPlus podría sustituir el `libSceAmpr.sprx` de
   ampr-emu-ng.

## 4. Qué comprobar (diseño 6.8)

- Arranca hasta el menú.
- Los vídeos se reproducen.
- Una partida carga.
- Guardar funciona.
- 10 minutos de juego sin tirones raros ni cuelgues.

Si algo falla, anota qué juego era, qué hacía en ese momento y, si tienes la versión de depuración
de ampr-emu-ng, su `/fsfail`. Un archivo que el juego abre con `mmap` no puede ir empaquetado: se
deja suelto volviendo a empaquetar con `--keep-loose "<ruta o patrón>"`.
