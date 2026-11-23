# MaxPack: programa con ventana para el PC (diseño)

*23 de noviembre de 2026. Es parte de la fase 4 del [diseño general](maxpack.md) («GUI»), adelantada
porque sin una ventana la mayoría de la gente no usará la herramienta. Se basa en
[NXPress](https://github.com/DeLFuS77/NXPress), el compresor de Switch del mismo autor.*

## 1. Intención

Que comprimir un juego de PS5 en el PC sea **elegir el juego, comprimirlo y que quede listo para la
PS5**, en una ventana como la de NXPress. Y lo mismo para la vuelta: descomprimir un `.p5z`.

Por qué (tus palabras): con comandos «es un rollo y la gente no lo aceptará». MaxPack y NXPress
tienen que parecer de la misma familia: misma ventana, mismo estilo, misma forma de trabajar
(decidido por ti: «la A, como NXPress»).

## 2. Objetivos, no-objetivos y anti-objetivos

**Objetivos**
- V1. **Un programa, como NXPress.** Sin argumentos abre la ventana; con argumentos es la línea de
  comandos de siempre (`maxpack pack …`).
- V2. **Comprimir.**
  - Se añaden uno o varios juegos (dumps en carpeta o imágenes `.exfat`) con botones o arrastrándolos.
  - Se elige el destino y «Hecho para»:
    - **Instalador MaxPack** (`.p5z`, la opción por defecto);
    - **Jugar desde el USB** (carpeta lista en `<destino>/homebrew/<TITLE_ID>`).
  - Se pulsa Comprimir. Cada juego se comprueba (`verify`) al terminar.
- V3. **Descomprimir** `.p5z` al dump original (bit a bit) o a la carpeta para la PS5.
- V4. **Verificar** `.p5z` y carpetas comprimidas, y **Detalles** de un juego (título, ID, versión,
  tamaños, packs, runtime).
- V5. Progreso por juego y en total, velocidad y tiempo restante, y **Cancelar** sin dejar nada a
  medias. Al acabar, la ventana de resultado: «X GB ahorrados», lo que más ahorró y qué hacer ahora.
- V6. Windows, macOS y Linux, en español e inglés y con tema claro y oscuro, como NXPress.
- V7. **Los extras esenciales de NXPress** (decidido por ti, «lo esencial»):
  - el PC no se duerme mientras hay un trabajo;
  - progreso en el icono de la barra de tareas (Windows) o del Dock (macOS);
  - aviso del sistema al terminar;
  - una sola ventana: abrir otra vez el programa o arrastrar juegos a su icono los añade a la ventana
    abierta;
  - recuerda el tamaño y la posición de la ventana.

**No-objetivos (por ahora)**
- Leer PKG, `.ffpkg`, `.ffpfs` y `.ffpfsc`. Van después, de uno en uno, empezando por PKG
  (decidido por ti). La ventana los aceptará cuando el motor los lea.
- Estimar o comparar niveles (`analyze`), enviar a la consola por red y las imágenes como salida.
- Actualización automática, integración con el Explorador, más idiomas y publicar versiones. Todo
  eso está en NXPress y se puede traer después.
- El resto de extras de NXPress: apagar al terminar, borrar el original tras comprimir, papelera,
  registro exportable, cuadrícula con orden y selección, preajustes, hilos y prioridad, y retomar la
  lista tras un cierre.

**Anti-objetivos**
- Un `.p5z` o una carpeta a medias que parezca bueno, por cancelar o por un error.
- Un juego que no arranca en la consola porque le falta ampr-emu-ng y nadie lo avisó.
- Sobrescribir algo sin preguntar.
- Que MaxPack se vea o se use distinto de NXPress sin motivo.

## 3. Restricciones

- **El motor sigue en C**, el de las fases 0 a 2: `pack`, `unpack`, `verify`, `info`. La ventana no lo
  reescribe.
- **NXPress** usa Go y Wails v2 (WebView2 en Windows, WebKitGTK en Linux, WKWebView en macOS), con una
  página en HTML, CSS y JS sin frameworks (`frontend/`), arrastrar y soltar (`EnableFileDrop`), una
  sola instancia y ajustes junto al programa o en la carpeta del usuario. Su `build.yml` compila e
  instala en Windows x64, Linux x64 y ARM64, y macOS con chip Apple e Intel.
- **Licencias:** NXPress es AGPL-3.0 y MaxPack, GPL-3.0-or-later. **Decidido por ti:** lo que se toma
  de NXPress (del que eres autor) se publica en MaxPack con la licencia de MaxPack, GPL-3.0-or-later.
- **Sin ampr-emu-ng** en `fakelib/`, el juego no arranca con ShadowMountPlus, y el instalador de la PS5
  todavía no lleva uno dentro (P4 de la fase 2).
- **FAT32** no admite archivos de más de 4 GiB: un `.p5z` grande no cabe. La carpeta sí, porque sus
  volúmenes miden menos.
- **Nombres:** en Windows `MaxPack.exe` y `maxpack.exe` son el mismo nombre de archivo, así que no
  pueden estar en la misma carpeta.

## 4. Enfoques considerados

- **A. Go + Wails, como NXPress, con el motor en C por debajo.** ← **Elegido por ti.** La ventana,
  el estilo, los idiomas, los instaladores y la compilación salen de NXPress.
- **B. Página local en el navegador** (C). Más rápido, pero es una pestaña, no se pueden arrastrar
  archivos y no se parece a NXPress.
- **C. Ventana propia en C** (librería webview). Hay que rehacer en C lo que Wails ya da hecho.

## 5. Diseño

### 5.1 Piezas

1. **`maxpack-core`**: el programa en C de siempre (la línea de comandos), con lo que la ventana
   necesita para hablar con él (5.2). En la versión para descargar va junto a la ventana con ese
   nombre, para no chocar con `MaxPack.exe`.
2. **`MaxPack`**: la ventana, en Go con Wails v2, en `gui/` dentro de este repositorio.
   - Sin argumentos, abre la ventana.
   - Con argumentos, ejecuta `maxpack-core` con esos argumentos, pasándole la consola, y devuelve su
     código de salida, como hace NXPress con la suya.
3. **La página** (`gui/frontend/`): parte de la de NXPress (estructura, estilos, temas y sistema de
   idiomas), con lo de Switch quitado y lo de MaxPack añadido.
4. **Compilación:** el flujo de GitHub compila `maxpack-core` con CMake y la ventana con Wails en
   Windows, macOS y Linux. Empaqueta como NXPress: instalador y versión portable en Windows, dmg en
   macOS, y archivo con instalador en Linux.

### 5.2 Cómo habla la ventana con el motor

La ventana ejecuta `maxpack-core` como proceso aparte, un trabajo a la vez:
- **`--progress json`** (en `pack`, `unpack` y `verify`): una línea JSON por evento en stdout, por
  ejemplo `{"event":"progress","phase":"packing","done":N,"total":M}`, y al final
  `{"event":"result",…}` con tamaños, ahorro y fallos. El texto para personas sigue en stderr.
- **`maxpack-core info --json <ruta>`**: qué es (dump, `.exfat` o `.p5z`), título, ID, versión,
  tamaño original e instalado, número de archivos, si trae un runtime que lea zstd, y si es un `.p5z`
  dañado.
- **`maxpack-core icon <ruta> -o <png>`**: saca `sce_sys/icon0.png` de un dump, un `.exfat` o un
  `.p5z`, para las portadas de la lista.
- **Cancelar:** con `--control stdin`, el motor lee su entrada. Si recibe `cancel` o se cierra, para
  en el siguiente bloque, borra lo que estaba escribiendo y sale con el código 130:
  - el `.p5z` a medias (ya lo hace `mp_pack_out_p5z`);
  - la carpeta de salida, que no existía o estaba vacía antes de empezar;
  - lo escrito de un dump original.

  Para eso el motor gana un enganche de progreso y de cancelación en `pack`, `unpack` y `verify`,
  sin cambiar lo que ve quien usa la línea de comandos.

### 5.3 La ventana (como NXPress)

```
┌ MaxPack 0.x ──────────────────────────────────────────────────────────────────────────────┐
│ Espacio        │  ⇣ Comprimir                                   │ Hecho para               │
│  Comprimir     │  Dumps de PS5 a .p5z o a carpeta lista.        │ (•) Instalador MaxPack   │
│  Descomprimir  │  [Añadir archivos] [Añadir carpeta] Vaciar     │     .p5z: lo más pequeño │
│ Comprobar      │  Juego                       Tamaño  Estado    │ ( ) Jugar desde el USB   │
│  Verificar     │  [ic] Hollow Knight Silksong 8,39 GB ● OK −72% │     carpeta en homebrew/ │
│  Detalles      │       PPSA12544, versión 01.000.010            │ Nivel de compresión  19  │
│                │       8,39 GB → 2,33 GB                        │ ─────────────────●──     │
│                │  [ic] RESIDENT EVIL requiem  133 GB  ● 37 %    │ Destino  E:\  [Elegir…]  │
│ Ajustes        │  …                                             │ ampr-emu-ng: ✓ (ruta)    │
│                │  1 de 2 hechos, 37 %  ▓▓▓░░░  410 MB/s  quedan 18 min       [Cancelar]  │
└───────────────────────────────────────────────────────────────────────────────────────────┘
```

- **Barra lateral:** Espacio (Comprimir, Descomprimir), Comprobar (Verificar, Detalles) y Ajustes.
- **Lista de juegos**, con portada (`icon`), nombre, ID y versión, tamaño, estado, una barra por juego
  y el ahorro. Se añaden con «Añadir archivos», con «Añadir carpeta» (que busca dumps y `.exfat` un
  nivel por debajo) o arrastrándolos a la ventana.
- **Panel derecho, en Comprimir:**
  - «Hecho para», con un texto corto de cada opción;
  - nivel de compresión (1–22, 19 por defecto, con atajos Rápido, Medio y Máximo como en NXPress);
  - destino;
  - estado de ampr-emu-ng.
- **Panel derecho, en Descomprimir:** «Dump original (bit a bit)» o «Carpeta para la PS5», y el destino.
- **Barra inferior:** total, porcentaje, velocidad, tiempo restante y Cancelar.
- **Al terminar,** la ventana de resultado:
  - «X GB ahorrados» y los juegos que más ahorraron;
  - qué hacer ahora: «Conecta el USB a la PS5 y ábrelo con el instalador MaxPack», o «ShadowMountPlus
    lo verá en homebrew/».
- **Temas claro y oscuro, e idiomas ES y EN,** con el sistema de NXPress.

### 5.4 Reglas al comprimir

- **Nombre del `.p5z`:** `<Título> [<TITLE_ID>].p5z`, sin caracteres que Windows no admita. Sin título,
  el nombre de la carpeta o la imagen.
- **Destino:**
  - si es la raíz de una unidad y el modo es carpeta, se usa `homebrew/<TITLE_ID>`;
  - si no, el `.p5z` o la carpeta van dentro de la carpeta elegida.
- **Destino ocupado:** si el `.p5z` ya existe o la carpeta no está vacía, se pregunta **Reemplazar**
  o **Saltar**, diciendo qué se borra y cuánto ocupa. Reemplazar sigue la regla de la fase 2: lo nuevo
  se escribe aparte (en `<destino>.maxpack-tmp`), se verifica, y solo entonces se aparta y se borra lo
  viejo. Por eso tienen que caber los dos; si no caben, se dice y no se empieza. Al cancelar o si
  falla, lo viejo sigue intacto.
- **El origen nunca es el destino:** si el juego que se comprime está en el sitio donde iría el
  resultado (por ejemplo, comprimir `E:\homebrew\PPSA12544` en modo carpeta hacia `E:\`), no se ofrece
  Reemplazar: se pide otro destino.
- **ampr-emu-ng:**
  - si un dump no lleva un `libSceAmpr.sprx` que lea zstd (`info --json` lo dice), se usa el de
    Ajustes con `--runtime`;
  - si en Ajustes no hay ninguno, se pide la ruta una vez, con una explicación corta;
  - si no se da, se avisa claro de que el juego no arrancará hasta ponérselo, y se puede seguir.
- **Espacio y FAT32:**
  - si el destino tiene menos espacio que el juego original, se avisa: «puede no caber»;
  - FAT32: el tamaño del `.p5z` no se sabe hasta el final, pero nunca pasa del original. Si el
    destino es FAT32 y el original pasa de 4 GiB, antes de empezar se avisa y se ofrece **Usar
    carpeta** (recomendado) o **Seguir igualmente**. Si al seguir el `.p5z` llega a 4 GiB, el motor
    para, borra lo escrito y lo dice. Si el original no pasa de 4 GiB, no hace falta avisar.
- **Juegos repetidos:** si dos juegos de la lista tienen el mismo ID y el mismo destino, se avisa antes
  de empezar.

### 5.5 Pruebas

- **Motor (CTest):**
  - `--progress json` en `pack`, `unpack` y `verify`: las líneas son JSON válido y el progreso acaba
    en el total;
  - `info --json` de un dump, un `.exfat`, un `.p5z` y uno dañado;
  - `icon` de los tres;
  - cancelar por stdin a mitad de cada trabajo: no queda nada a medias y el código es 130;
  - la salida para personas no cambia (las pruebas de la línea de comandos siguen en verde).
- **Ventana (Go, como las pruebas de NXPress):**
  - el lanzador de trabajos con el `maxpack-core` real y los dumps de prueba;
  - nombres de `.p5z` y destinos;
  - destino ocupado;
  - aviso de FAT32 (con un enganche que simula el sistema de archivos);
  - cancelar;
  - con argumentos, se comporta como la línea de comandos.
- **Página:** las mismas comprobaciones que hace NXPress con la suya (textos ES y EN, nada de
  `innerHTML` con datos).
- **A mano:**
  - comprimir y descomprimir los dumps de prueba y Silksong, cancelar, temas e idiomas, y arrastrar
    archivos;
  - el instalador de Windows desde el artefacto de GitHub.

## 6. Decisiones que he tomado yo (puedes cambiarlas)

- La ventana vive en `gui/` de este repositorio, no en otro aparte, para que motor y ventana se
  publiquen juntos.
- Se habla con el motor ejecutándolo como proceso (`--progress json` y `--control stdin`), no
  enlazándolo con cgo. Así se compila igual de fácil en los cinco sistemas y la línea de comandos queda
  como está.
- **Nombres:** el motor se llama `maxpack-core` junto a la ventana `MaxPack`; el `maxpack` de siempre
  sigue existiendo solo para quien use solo la línea de comandos.
- Al empezar, la ventana solo está en español e inglés.
- Sin Pausar: Cancelar para el juego en marcha y deja los demás en espera.
- Reemplazar escribe aparte y borra lo viejo al final, con lo nuevo verificado, aunque eso pida
  espacio para los dos.
- El origen nunca puede ser el destino.
- FAT32: aviso por el tamaño del original y parada limpia si el `.p5z` llega a 4 GiB, en vez de
  estimar el tamaño antes.
- Los extras de V7 se rehacen alrededor de `maxpack-core` siguiendo cómo los hace NXPress.
- Nivel 19 por defecto, con los atajos de NXPress.

## 7. Lo que queda al criterio del constructor

Cómo se reparte la página de NXPress (qué se copia y qué se rehace), nombres internos, el formato
exacto de las líneas JSON, el intervalo de progreso, iconos y textos, cómo se guardan los ajustes
(como NXPress) y cómo se ordena el flujo de compilación.
