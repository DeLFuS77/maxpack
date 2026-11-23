# MaxPack fase 2 — instalador en la PS5 (diseño)

*23 de noviembre de 2026. Desarrolla la sección 6.5 de
[maxpack.md](maxpack.md), que sigue siendo la referencia general.*

## 1. Intención

Que instalar un juego comprimido con MaxPack sea **un paso en la consola**: eliges el `.p5z`, pulsas
Instalar y el juego aparece en ShadowMountPlus (o en Kraken Mount) listo para jugar, todavía comprimido.

Por qué (tus palabras): si hay que dar pasos extra para jugar, "es un rollo y la gente no lo
aceptará". Hoy el `.p5z` no lo entiende ningún cargador; hay que extraer la carpeta en el PC, copiarla,
poner el runtime y tocar el fakelib a mano. El instalador hace todo eso solo.

También instala, tal cual, los juegos en los formatos de siempre (carpetas e imágenes). En la consola
solo se dejan los juegos listos para jugar: **nunca se comprime allí** (decidido por ti); comprimir es
cosa del PC.

## 2. Objetivos, no-objetivos y anti-objetivos

**Objetivos**
- I1. Instalar un `.p5z` que esté en un USB, en el almacenamiento ampliado o en el interno, desde una
  página web servida por la propia consola. El resultado es la misma carpeta que da
  `maxpack unpack --folder`, con el runtime puesto.
- I2. Compatible con lo que la gente usa: el juego queda en una ruta que exploran ShadowMountPlus y
  Kraken Mount; si ShadowMountPlus está en marcha, se registra y se le fija el fakelib.
- I3. Seguro: comprueba el espacio antes de empezar, verifica el SHA-256 de todo lo que escribe y
  sigue donde se quedó si se corta.
- I4. Gestor mínimo de lo instalado con MaxPack: lista, Verificar y Desinstalar.
- I5. Página en español e inglés, según el idioma del navegador.
- I6. Instalar tal cual (decidido por ti) carpetas de juego, sean dumps o carpetas con packs de Dump
  Forge, e imágenes `.exfat`, `.ffpkg`, `.ffpfs` y `.ffpfsc`. Se copian verificadas, con el mismo
  espacio, reanudación, reemplazo y registro que un `.p5z` (5.4 bis).

**No-objetivos (en esta fase)**
- Instalar por SMB o con `maxpack send` desde el PC (fase 2 bis, después de esta).
- Comprimir en la consola (decidido por ti); instalar PKG (eso lo hacen ShadowMountPlus y etaHEN con
  el instalador de Sony); juegos de PS4.
- Gestionar juegos que no instaló MaxPack, salvo sustituirlos por su versión comprimida al instalarla
  (5.4, paso 2).
- Formato v2 y runtime propio (fase 3).

**Anti-objetivos (sería un fracaso aunque "funcione")**
- Que ShadowMountPlus o Kraken Mount vean un juego a medio instalar o sin verificar.
- Borrar el `.p5z` antes de que el juego instalado esté verificado.
- Sobrescribir o borrar un juego sin preguntar, o tocar archivos de otros juegos.
- Cambiar ajustes globales de ShadowMountPlus (afectarían a todos los juegos).
- Que la página permita leer o borrar rutas arbitrarias de la consola.

## 3. Restricciones

- **SDK:** ps5-payload-dev/sdk (GPL-3): clang-18 y lld-18, compilación con Makefile, solo en Linux o
  macOS. Este PC no tiene WSL. pacbrew-repo tiene zstd 1.5.6 y libmicrohttpd 1.0.1.
- **Carga:** el payload se envía al cargador de ELF del puerto 9021 (elfldr, etaHEN, Payload Manager).
  Puertos ya ocupados por la escena: 8080 (websrv), 2121 (ftpsrv), 9120/9180/19113 (ps5upload),
  10101 (ShadowMountPlus). El instalador usa el **8095**.
- **ShadowMountPlus 1.7:** API HTTP/JSON en `0.0.0.0:10101`, sin autenticación, todo `POST`.
  `/api/v1/version`, `/api/v1/manual/add {"path"}` (idempotente), `/api/v1/scan` (puede quedar
  aplazado: `scan_deferred_reason` = `game_active`, `runtime_prepared`, `rest_mode`),
  `/api/v1/games`, `/api/v1/games/fakelib {"title_id","mode"}` con modos `full`, `emulators`,
  `disabled` y `default`. Su actualizador de AMPR baja el `ampr_emu` antiguo de drakmor, que no lee
  packs zstd.
- **Kraken Mount:** trae ampr-emu-ng incluido, no puede funcionar a la vez que ShadowMountPlus y solo
  explora un nivel por debajo de cada raíz.
- **Rutas que exploran los dos:** `/data/homebrew`, `/data/etaHEN/games`, `/mnt/ext0..1` y
  `/mnt/usb0..7` (y sus `homebrew/` y `etaHEN/games/`).
- **Runtime:** ampr-emu-ng (GPL-3). Su página no muestra releases; hay copias en Dump Forge
  (`vendor/ampr-emu-ng/libSceAmpr.sprx`) y dentro de Kraken Mount.
- **Tamaños:** los volúmenes `.pak` miden como mucho 4.294.901.760 bytes, así que caben en FAT32; un
  `.p5z` grande (79 GB en RE Requiem) necesita un USB en exFAT.
- **Pruebas:** en consola solo las puede hacer el usuario. La prueba en consola de la fase 1 todavía
  no se ha hecho. El trabajo en el PC (5.8, pasos 1 y 2) puede empezar ya; esa prueba tiene que pasar
  antes del paso 4, y lo que salga de ella (sección 7) puede cambiar el runtime y la parte de
  ShadowMountPlus.

## 4. Enfoques considerados

1. **Imagen montable** (`.ffpkg`/`.exfat` con los packs dentro) que ShadowMountPlus monta sin
   programa nuestro. Más simple de adoptar, pero ocupa algo más que el `.p5z`, se lee más despacio que
   una carpeta y no se sabe si ampr-emu-ng funciona dentro de una imagen montada.
2. **Instalador propio en la consola.** ← **Elegido por ti.** Deja la carpeta, que es lo más rápido de
   leer, y mantiene el `.p5z` como el archivo más pequeño. Cuesta cargar un payload más y las pruebas
   en consola.
3. **Pedir a ShadowMountPlus, Kraken Mount o ps5upload que acepten `.p5z`.** No depende de nosotros.

Descartado también un `maxpack send` solo en el PC (FTP + API de ShadowMountPlus): obliga a tener el PC
encendido y no sirve para un `.p5z` que ya está en un USB. Queda para la fase 2 bis como forma de envío.

## 5. Diseño

### 5.1 Piezas

1. **`libmaxpack`** (lo que ya existe), compilado también para la PS5: lector de `.p5z`, packs,
   `ampr_emu.index`, hashes y zstd 1.5.6. El código POSIX de `mp_platform.c` es el punto de partida.
2. **Motor de instalación** (nuevo, C11 portable): busca `.p5z`, planifica, copia, verifica, reanuda,
   lleva el registro de lo instalado, verifica y desinstala. Funciona igual en el PC y en la PS5.
3. **Servidor web y página** (nuevo, portable): HTTP mínimo con una API JSON y la página (HTML, CSS y
   JS) incrustada en el binario.
4. **Capa PS5** (nueva, solo consola): arranque del payload, avisos en pantalla, espacio libre,
   unidades montadas y cliente de la API de ShadowMountPlus.
5. **Versión de PC del instalador** (para pruebas): el motor y el servidor web compilados en Windows o
   Linux, con un ShadowMountPlus falso. No se distribuye; sirve para probar casi todo sin consola.
6. **Compilación del `.elf` en GitHub Actions** (Ubuntu, con el SDK y pacbrew) en cada push, con el
   `.elf` como artefacto. Así no hace falta WSL en el PC.

### 5.2 La página

Se abre en `http://<IP de la PS5>:8095` desde el navegador de la PS5, el móvil o el PC. El navegador de
la PS5 es un WebKit antiguo: JS simple, sin frameworks, y el progreso se pide cada segundo.

```
MaxPack · instalador                          192.168.1.50:8095   ES | EN
Juegos encontrados                                              [Buscar otra vez]
  [icono] Hollow Knight Silksong   PPSA12544 v01.000.010   2,33 GB   USB0
  [icono] RESIDENT EVIL requiem    PPSA31246 v01.310.000  79,32 GB   USB0
  Ruta manual: [____________________]
Destino: (•) Interno /data/homebrew — 512 GB libres  ( ) USB0 /mnt/usb0/homebrew — 1,2 TB libres
[ ] Borrar el .p5z cuando esté instalado y verificado
[ Instalar ]
Instalando RESIDENT EVIL requiem · 37 % · 412 MB/s · quedan 3 min      [Pausar]
Instalados con MaxPack
  Hollow Knight Silksong  PPSA12544  /data/homebrew/PPSA12544  ✓   [Verificar] [Desinstalar]
```

- El título, la versión y el icono salen del `.p5z` (manifiesto y `sce_sys/icon0.png`).
- Los textos que vienen del juego (nombres) se escapan siempre: son datos no fiables.
- Desinstalar y Reemplazar piden confirmación.

### 5.3 Dónde busca

En la raíz y un nivel por debajo de `/mnt/usb0..7` y `/mnt/ext0..1`, y en `/data/maxpack/`, busca:
- archivos `.p5z`;
- carpetas de juego (con `sce_sys/param.json`);
- imágenes `.exfat`, `.ffpkg`, `.ffpfs` y `.ffpfsc`.

La página dice de qué tipo es cada uno. Además se puede escribir una ruta a mano; la API solo acepta
rutas de esos tipos dentro de esas unidades o de `/data`.

### 5.4 Instalar

1. **Abrir y validar** el `.p5z` (cabecera, pie e índice, como `mp_p5z_open`). Uno dañado se rechaza
   antes de escribir nada.
2. **Destino:** `/data/homebrew/<TITLE_ID>`, `/mnt/extN/homebrew/<TITLE_ID>` o
   `/mnt/usbN/homebrew/<TITLE_ID>`. Se busca si el juego ya está en la consola: una carpeta en el
   destino, o el mismo `TITLE_ID` en otra ruta de las que exploran los cargadores (leyendo su
   `sce_sys/param.json`, y en la lista de ShadowMountPlus si está en marcha). **Decidido por ti:** se
   puede sustituir aunque no lo instalara MaxPack (por ejemplo, el dump sin comprimir), para que quede
   una sola copia de cada juego.
   - Se pregunta **Reemplazar** o Cancelar, diciendo qué carpeta se borra y cuánto ocupa.
   - Lo viejo solo se borra cuando lo nuevo está instalado y verificado (paso 8).
   - Nunca se sobrescribe ni se borra nada sin esa confirmación.
3. **Espacio:** hace falta el tamaño total de la carpeta final (lo dice el índice) más un margen; si
   no cabe, se dice cuánto falta y no se empieza. Aunque esté marcado "borrar el `.p5z`", el espacio
   se cuenta entero, porque el `.p5z` se borra al final. Si se reemplaza un juego del mismo disco,
   tienen que caber los dos a la vez.
4. **Carpeta temporal fuera de las rutas que exploran los cargadores** y en el mismo sistema de
   archivos que el destino, para poder renombrar al final: `/data/maxpack/tmp/<TITLE_ID>` para el
   interno y `/mnt/<unidad>/maxpack-tmp/<TITLE_ID>` para las demás. Así ningún cargador ve un juego
   a medias.
5. **Escritura:** los volúmenes `.pak` se copian tal cual y se comprueba el SHA-256 que guarda el
   `.p5z`; los sueltos se descomprimen y se comprueba su SHA-256 del índice; después van los índices.
   El resultado tiene que ser idéntico byte a byte a `maxpack unpack --folder`.
   **Qué es "verificado":** el SHA-256 se calcula sobre los datos al escribirlos, y cada archivo se
   fuerza a disco (`fsync`) antes de apuntarlo como terminado. Si está marcado "borrar el `.p5z`",
   antes de borrarlo se relee la carpeta instalada entera y se comprueba otra vez, porque después será
   la única copia. Eso dobla la lectura (79 GB más en RE Requiem), pero solo en ese caso.
6. **Runtime:** si el `.p5z` trae `fakelib/libSceAmpr.sprx` y lee packs zstd (marcador `AMPRZPK1`),
   se usa ese. Si no trae, o trae el `ampr_emu` antiguo, se pone la copia de ampr-emu-ng que lleva el
   instalador y se avisa. En ese caso **hay que regenerar `ampr_emu.index`** con la fila del runtime
   nuevo (tamaño y fecha), igual que hace `pack --runtime`: la lista completa de archivos está en el
   índice del `.p5z`. **Es el caso normal, no una excepción:** los dumps de Silksong y Wukong traen
   el `ampr_emu` antiguo.
7. **Reanudar:** un archivo de estado en `/data/maxpack/state/<TITLE_ID>.json` apunta cada archivo
   terminado y verificado. Si se corta (apagón, reposo, USB fuera, payload cerrado), al volver a
   pulsar Instalar con el mismo `.p5z` (mismo `archive_id`) los archivos terminados se vuelven a
   comprobar leyéndolos del destino y se conservan los que coinciden; el resto se rehace. El tamaño
   solo no basta: tras un apagón, un USB con caché puede tener el tamaño bien y el contenido sin
   escribir. Con otro `.p5z` para el mismo título se empieza de cero.
8. **Terminar:** se renombra la carpeta temporal al destino (en Reemplazar: si el viejo está en el
   mismo sitio, se aparta, se pone el nuevo y luego se borra el viejo; si está en otra unidad, se
   borra después; en los dos casos se pide a ShadowMountPlus que vuelva a escanear) y se guarda el registro
   `/data/maxpack/installed/<TITLE_ID>.json` (título, versión, ruta, `archive_id`, runtime, fecha).
   **Dentro de la carpeta del juego no se añade ningún archivo de MaxPack.**
9. **ShadowMountPlus:** si `127.0.0.1:10101/api/v1/version` responde, primero se fija el fakelib del
   juego (`games/fakelib`, con el modo que deje funcionar a ampr-emu-ng, ver P1) y después se llama a
   `manual/add` con la ruta final y a `scan`. El juego cuenta como **instalado** en cuanto la carpeta
   está verificada en su sitio; lo de ShadowMountPlus es un paso aparte:
   - si alguna llamada falla o el escaneo queda aplazado, se apunta como pendiente en el registro del
     juego y se reintenta al arrancar el payload y cada minuto mientras esté cargado;
   - la página muestra "pendiente en ShadowMountPlus", con el motivo y cómo hacerlo a mano.

   Si ShadowMountPlus no está en marcha, se avisa de que lo verá en su próximo escaneo, o Kraken Mount.
10. **Borrar el `.p5z`**, solo si estaba marcado y todo está verificado.
11. **Aviso en la consola:** "Silksong instalado" (o el error).

Solo hay una instalación a la vez; otra pulsación se pone en cola.

### 5.4 bis Instalar tal cual (carpetas e imágenes)

Mismos pasos que 5.4 (destino, copias existentes y Reemplazar, espacio, temporal, reanudar, registro
y ShadowMountPlus), con estas diferencias:
- **El juego:**
  - en una carpeta o una imagen `.exfat`, el `TITLE_ID`, el nombre y la versión salen de
    `sce_sys/param.json` (el lector de exFAT ya existe);
  - en `.ffpkg`, `.ffpfs` y `.ffpfsc`, el `TITLE_ID` sale del nombre del archivo (por ejemplo
    `Silksong-PPSA12544.ffpkg`). Si no lo lleva, no se instala y se dice que se añada al nombre.
- **Destino:** una carpeta va a `<raíz>/homebrew/<TITLE_ID>`; una imagen, a `<raíz>/homebrew/` con su
  nombre de archivo. Una imagen con el mismo `TITLE_ID` en su nombre cuenta como copia existente.
- **Copia:** byte a byte, con el SHA-256 de cada archivo calculado al leerlo, `fsync` y la fecha
  original. Los SHA-256 se guardan junto al registro, para que Verificar y la reanudación tengan con
  qué comparar.
- **Listo para jugar:** si una carpeta trae packs zstd (`ampr_zstd.index`) y le falta ampr-emu-ng (o
  trae el `ampr_emu` antiguo), se pone el runtime del instalador y se cambia su fila en
  `ampr_emu.index`, como en 5.4 paso 6. Una imagen no se toca.
- **ShadowMountPlus:** el fakelib solo se fija en carpetas con packs; para el resto basta con
  `manual/add` y `scan`.
- **El original** (en el USB) no se borra: no hay opción para ello.

### 5.5 Verificar y desinstalar

- **Verificar** comprueba la carpeta instalada como `maxpack verify <carpeta>`: índices, frames zstd
  con su checksum y los sueltos contra el `.p5z` si sigue disponible. En lo instalado tal cual, relee
  cada archivo y lo compara con los SHA-256 guardados.
- **Desinstalar** solo funciona con juegos del registro de MaxPack, pide confirmación, borra la
  carpeta y el registro y pide a ShadowMountPlus que vuelva a escanear.
- Ni Reemplazar ni Desinstalar se hacen mientras ese juego está en marcha (si ShadowMountPlus lo dice).

### 5.6 Errores

Cada error se muestra en la página y en un aviso de la consola, con qué archivo y qué pasó, y la
instalación queda en un estado del que se puede reanudar o que se puede descartar:
- disco lleno a mitad, USB quitado, `.p5z` dañado (SHA-256 que no coincide): se para, nunca se
  renombra al destino;
- puerto 8095 ocupado (otro MaxPack cargado): aviso y salida;
- ruta pedida fuera de las unidades permitidas: rechazada.

### 5.7 Pruebas

**En el PC (CTest, sin consola)**
- Instalar un `.p5z` de prueba y comparar con `unpack --folder` byte a byte.
- Cortar la instalación en puntos al azar (fallo inyectado tras N bytes) y reanudar: resultado idéntico.
- Disco lleno simulado, `.p5z` con un byte cambiado, destino existente sin confirmar: no aparece
  carpeta final y no se toca nada fuera de la temporal.
- Runtime sustituido: `ampr_emu.index` idéntico al que da `pack --runtime` con ese runtime.
- Reemplazar un dump sin comprimir: sin confirmación no se toca; con confirmación, el viejo solo se
  borra después de verificar el nuevo; si no caben los dos, no empieza; el mismo `TITLE_ID` en otra
  unidad (con otro nombre de carpeta) se detecta.
- ShadowMountPlus falso: se reciben `manual/add`, `scan` y `games/fakelib` con los datos correctos;
  sin ShadowMountPlus, la instalación termina igual.
- API web: rutas fuera de lo permitido rechazadas; nombres con `<script>` escapados.
- Tal cual:
  - una carpeta de dump queda idéntica al original (bytes y fechas);
  - una carpeta con packs y sin ampr-emu-ng recibe el runtime y su `ampr_emu.index` queda como el de
    `pack --runtime`;
  - una imagen `.exfat` y una `.ffpkg` con el ID en el nombre se copian idénticas; una sin ID se rechaza;
  - el corte y la reanudación funcionan igual, y Verificar detecta un byte cambiado.

**En GitHub Actions:** el `.elf` compila y enlaza con el SDK.

**En consola (lo haces tú), después de que pase la prueba de la fase 1**
- Cargar el payload y abrir la página desde la PS5 y desde el móvil.
- Instalar Silksong del USB al interno, quitar el USB a mitad, volver a ponerlo y reanudar.
- Que ShadowMountPlus lo registre y se juegue (la lista de la fase 1); lo mismo con Kraken Mount.
- Desinstalar. Instalar RE Requiem (79 GB) y anotar el tiempo.

### 5.8 Orden de trabajo

1. Motor de instalación y pruebas en el PC.
2. Servidor web y página, en el PC.
3. Capa PS5 y compilación en GitHub Actions.
4. Prueba en consola.

Después, la fase 2 bis: instalar desde SMB y `maxpack send` desde el PC.

## 6. Decisiones que he tomado yo (puedes cambiarlas)

- El instalador lleva su propia copia de ampr-emu-ng, con la licencia GPL-3 y el crédito a sus autores.
- Carpeta temporal fuera de las rutas de los cargadores y registro en `/data/maxpack/`, sin añadir
  archivos dentro del juego.
- Opción "borrar el `.p5z` al terminar", desmarcada por defecto.
- Página en español e inglés.
- Servidor HTTP propio y mínimo (sockets), para no depender de libmicrohttpd en el PC; se puede
  cambiar por libmicrohttpd si en la consola hace falta.
- Progreso por consulta cada segundo, pensando en el navegador de la PS5.
- Compilación del `.elf` en GitHub Actions en vez de instalar WSL.
- "Verificado" = SHA-256 al escribir y `fsync`; se relee todo solo antes de borrar el `.p5z`, al
  pulsar Verificar y, al reanudar, en los archivos ya terminados.
- "Instalado" = carpeta verificada en su sitio; lo de ShadowMountPlus se reintenta aparte.
- El trabajo en el PC empieza sin esperar a la prueba en consola de la fase 1.
- Tal cual: el `TITLE_ID` de `.ffpkg`, `.ffpfs` y `.ffpfsc` se saca del nombre del archivo, porque
  aún no hay lector de UFS2 ni de PFS; sin opción de borrar el original; una carpeta con packs recibe
  ampr-emu-ng si no lo trae; los SHA-256 de lo copiado se guardan para poder verificarlo.

## 7. Pendiente de comprobar en consola (con la prueba de la fase 1)

- **P1.** Qué modo de fakelib de ShadowMountPlus (`full`, `emulators`, `disabled`) deja que el juego
  cargue su `fakelib/libSceAmpr.sprx` sin que ShadowMountPlus lo cambie por el suyo; si ese modo se
  puede fijar antes de que ShadowMountPlus conozca el juego, y si un escaneo hecho antes de fijarlo
  ya le pone su runtime.
- **P2.** Qué hace ShadowMountPlus con un juego cuya carpeta desaparece (desinstalar), y si tiene una
  llamada para quitarlo.
- **P3.** Si el payload sobrevive al modo reposo (si no, basta con reanudar).
- **P4.** De qué copia de ampr-emu-ng se parte (la de Dump Forge o la de Kraken Mount) y su versión.

## 8. Lo que queda al criterio del constructor

Nombres de los módulos y de las rutas de la API, formato exacto de los archivos de estado y de
registro, cuántos hilos usar en la consola, el margen de espacio, cómo se reparte la compilación
entre CMake y el Makefile del SDK, el diseño visual de la página y los textos.
