# MaxPack: rediseño de las dos interfaces (diseño)

*24 de noviembre de 2026. Cambia el aspecto de la ventana del PC ([interfaz-pc.md](interfaz-pc.md),
[ventana-pc.md](../ventana-pc.md)) y de la página de la consola (`src/ui.html` de maxpack-ps5, puerto
8095). No cambia lo que hacen.*

## 1. Intención

Tus palabras: «mejorar tanto UX UI de todo que se vea lo más llamativo y bonito posible, funcional y
con animaciones llamativas si se puede».

Hay dos interfaces:
- **la ventana del PC**, para comprimir, descomprimir, verificar y ver detalles;
- **la página de la consola**, con la biblioteca, el instalador y los ajustes. Se abre en el
  navegador de la PS5, con el mando, y también en el móvil.

Viste tres maquetas: A «Ambiente», B «Bloques de color» y C «Neón». **Elegiste la A.**

## 2. Objetivos, no-objetivos y anti-objetivos

**Objetivos**
- R1. **La dirección A en las dos interfaces.** Es como el menú de la PS5: un fondo que toma el
  color del juego, el azul del icono de MaxPack con un degradado a cian para lo que avanza y verde
  menta para el espacio ahorrado.
- R2. **La consola, más viva:**
  - el fondo toma el color del juego enfocado;
  - el juego enfocado sale en grande arriba;
  - las tarjetas crecen al enfocarlas, con anillo blanco y halo de su color;
  - las pestañas tienen una píldora blanca que se desliza;
  - las barras de progreso tienen un brillo que las recorre;
  - la ficha del juego y los diálogos aparecen con una animación.
- R3. **El PC, más vivo, sin moverle nada de sitio:**
  - el resaltado del menú se desliza de una sección a otra;
  - los juegos entran en la lista con una animación;
  - las barras tienen brillo, y el «−72 %» aparece con un salto;
  - los diálogos aparecen con una animación;
  - el final de una compresión es el momento estrella: la barra del tamaño se aprieta hasta el
    tamaño nuevo y los GB ahorrados cuentan hacia arriba, con un pequeño estallido.
- R4. **Animaciones que la PS5 mueve sin tirones.** Solo con `transform` y `opacity`. Con
  `prefers-reduced-motion`, nada se mueve y todo sigue funcionando.

**No-objetivos**
- Cambiar funciones, textos, el orden de las cosas o la distribución de las pantallas. Lo único nuevo
  que se ve es el nombre grande del juego enfocado en la consola. Repite datos que ya están en la
  tarjeta.
- Cambiar NXPress. MaxPack deja de verse como NXPress; llevarle este aspecto es otro trabajo.
- Fuentes propias. Se usan las del sistema.
- Un tema claro en la consola. Sigue siendo solo oscura, como ahora.

**Anti-objetivos**
- Tirones o lentitud en la PS5 por culpa de los efectos.
- Perder algo: una función, un texto, un idioma, la lectura de derecha a izquierda (árabe), el foco
  visible con el mando o lo que lee un lector de pantalla.
- Que se lea peor: menos contraste del que pide WCAG AA para el texto.
- El CI en rojo, o algo cargado de internet.

## 3. Restricciones

**Página de la consola** (`src/ui.html`, un solo archivo):
- **El navegador de la PS5 es un WebKit antiguo.** No hay `aspect-ratio`, `:has()`, `color-mix()`,
  `inset` ni `@starting-style`. Sí hay `async`/`await`, flex con `gap` y `transform`. El código
  existente ya evita esas cosas, y el nuevo sigue igual.
- **Se usa con el mando** (D-pad, L2/R2) en una tele de 1920 × 1080, y con el dedo en el móvil.
- **Sin conexión.** Usa appcache y no hace peticiones fuera. El CSS y el JS van dentro del archivo.
- **11 idiomas, árabe incluido.** No se quita ninguna clave de `MSG`. Si se añade alguna, va al menos
  en `en`, que es el idioma de respaldo.
- **CI.**
  - `node --check` del `<script>`.
  - `tools/check_identity.sh`, que exige que la línea del enlace de crédito (`<a class="gh-link
    button" data-credit="kraken-mount" …>`) y las de `aboutBased` sigan **exactamente** iguales.
  - `node --check tools/mock_ui.js`, comprobado a mano.

**Ventana del PC** (`gui/frontend`):
- **La política de seguridad (CSP) es `default-src 'self'`.** En el HTML no puede haber atributos
  `style=` ni etiquetas `<style>`. Desde JS sí se puede poner `element.style.transform`.
- **`gui/frontend_test.go`** exige:
  - que `innerHTML` reciba solo plantillas fijas;
  - que los datos entren con `textContent`;
  - que no se cargue nada de fuera;
  - que es.js y en.js tengan las mismas claves, y que cada texto que se pida esté en los dos;
  - que estén los ids, las secciones y los diálogos de siempre, colgando del `<body>`;
  - que los scripts carguen en su orden;
  - que cada archivo tenga la cabecera SPDX.
- **Corre en tres motores web:** WebView2 en Windows, WKWebView en macOS y WebKitGTK en Linux. Tiene
  **tema claro y oscuro**, el del sistema o el que se elija en Ajustes.

## 4. Enfoques

| | Cómo es | A favor | En contra |
|---|---|---|---|
| **A · Ambiente** (elegida) | Fondo del color del juego enfocado, azul a cian, verde menta | Se siente como la propia PS5; mantiene el azul de MaxPack; sus efectos son baratos (degradados y opacidad) | Depende de sacar el color de cada juego (con un respaldo) |
| B · Bloques de color | Un color sólido por pantalla y por sección, formas grandes, sin cristal | La más ligera para la PS5 | Menos «PS5»; colores nuevos que no salen del icono |
| C · Neón | Arcade: rosa y cian con brillo, rejilla en movimiento | La más llamativa | Cansa la vista, peor contraste, la más pesada para la PS5 |

## 5. Diseño

### 5.1 Fichas comunes

**Colores, tema oscuro** (la consola y el PC en oscuro):

| Nombre | Valor | Para qué |
|---|---|---|
| Noche | `#07101f` (consola), `#0b111d` (PC) | fondo |
| Superficie | `#111827` / `#172033` (PC); blanco al 6–12 % (consola) | paneles, filas, tarjetas |
| Línea | `#222d42` | bordes y separadores |
| Texto | `#eef3ff` | texto |
| Apagado | `#93a0b8` | texto secundario |
| Azul MaxPack | `#2d7dff` | elegir, empezar, foco |
| Cian | `#36e2ff` | final del degradado de lo que avanza |
| Menta | `#3fe3a2` | espacio ahorrado, OK |
| Ámbar | `#ffb547` | avisos |
| Coral | `#ff5c73` | errores y lo que no se deshace |

**Tema claro del PC:** fondo `#eef2f8`, paneles blancos, línea `#dde3ec`, texto `#111a2b`, apagado
`#5b667a`, azul `#1b6ef3`, cian `#0a9fd0`, menta `#0f9468`, ámbar `#9a5a00`, coral `#d02a43`. El
degradado azul a cian y su brillo son los mismos.

**Letra:** la del sistema. «Segoe UI Variable Display» para los títulos y los números grandes, con
peso 700–800 y letras algo más juntas (`letter-spacing` −0,02 a −0,04 em). Las cifras usan
`tabular-nums`. Los títulos de grupo pasan de MAYÚSCULAS a frase normal en negrita.

**Movimiento:**
- Entradas y foco: 0,3–0,45 s con un rebote suave, `cubic-bezier(.3, 1.35, .5, 1)`.
- Fundidos: 0,8–1,1 s.
- Brillo de las barras: 1,6–1,8 s en bucle.
- Todo con `transform` y `opacity`. Las barras dejan de animar `width` y pasan a `transform:
  scaleX()`.
- Con `prefers-reduced-motion` no hay animaciones ni transiciones, como ahora.

### 5.2 La página de la consola

- **Fondo ambiente.** Dos capas fijas detrás de todo, cada una con degradados radiales del color del
  juego. Al cambiar de juego, una se funde en la otra con `opacity` en 1,1 s.
  - El color sale del icono del juego (`/icon/<TID>`, del mismo origen). Se pinta en un canvas
    pequeño y se queda el color medio más saturado. Se calcula una vez por juego y se guarda.
  - Si no hay icono o falla, sale del matiz que ya da `gameHue(title_id)`.
  - Sin juego enfocado, el fondo es el azul de MaxPack.
  - Sobre las capas va un velo que oscurece hacia abajo para que el texto se lea.
- **Menos cristal.** Hoy casi todo usa `backdrop-filter` (desenfoque), y con un fondo que cambia
  costaría mucho en la PS5. Pasa a rellenos translúcidos sin desenfoque. Solo lo conserva el fondo
  de la ficha del juego y de los diálogos, que no se mueven mientras están abiertos.
- **Cabecera.**
  - El icono de MaxPack, con el brillo de ahora.
  - Las pestañas con una píldora blanca que se desliza con `translateX` + `scaleX` hasta la pestaña
    elegida.
  - El botón Detener.
- **Nombre grande (nuevo, solo en la biblioteca y en pantallas de 860 px o más):**
  - el nombre del juego enfocado, a unos 50 px en la escala de 1920, con peso 800;
  - debajo, su ID, versión, tipo y estado, con las mismas etiquetas que ya usa la tarjeta;
  - al cambiar de juego, el texto se funde y sube;
  - lleva `aria-hidden="true"`, porque la tarjeta ya lo dice;
  - sin foco en una tarjeta se queda el último; sin juegos, no se ve.
- **Filtros y unidades.** Las mismas píldoras y medidores, sin desenfoque. El medidor se dibuja con
  `scaleX` y el degradado azul a cian.
- **Tarjetas.**
  - En la PS5 el mando mueve un cursor (la página no gestiona las flechas allí, `PS`), así que «la
    tarjeta enfocada» es la que tiene el foco **o el cursor encima**. Con el mando en un PC o con el
    teclado es el foco; en la PS5, el cursor. El fondo ambiente y el nombre grande siguen a la
    última de las dos cosas que haya pasado.
  - La tarjeta enfocada crece a 1,08, con un anillo blanco de 3 px más un aro tenue y un halo de su
    color debajo. El anillo y el halo son capas que se funden con `opacity`.
  - Mientras hay una tarjeta enfocada, las demás bajan al 85 %. Lo controla una clase que pone JS,
    porque `:has` no existe en la PS5.
  - La franja del nombre pasa a un degradado oscuro, sin desenfoque.
  - Las píldoras de tipo y estado se quedan donde están.
- **Ficha del juego y diálogos.** El fondo aparece fundido y la hoja entra creciendo de 0,96 a 1. Es
  una animación con `@keyframes` al quitarse `hidden`.
- **Instalar.**
  - El trabajo en curso lleva la portada del juego (de su icono, o sus iniciales), el porcentaje en
    grande y una barra gruesa con degradado y brillo.
  - El elegido de la lista crece un poco y lleva anillo blanco.
  - El botón Instalar lleva el degradado azul.
  - Los títulos de grupo van en frase normal.
- **Ajustes.** Las mismas filas, sobre las superficies nuevas. Los interruptores y botones siguen
  igual.
- **Avisos.** Entran deslizando desde arriba. Llevan los colores de aviso y error nuevos.
- **Móvil (< 860 px).** Sin nombre grande y sin atenuar las demás tarjetas. Fondo ambiente fijo en
  azul. Se mantienen los cortes de ahora (4, 3 y 2 columnas).
- **Árabe.** Se mantienen las reglas `[dir="rtl"]`. La píldora y el halo funcionan igual en los dos
  sentidos.

### 5.3 La ventana del PC

Cada cosa sigue en su sitio: secciones a la izquierda, lista en el centro, opciones a la derecha y
progreso abajo.

- **Menú.**
  - Un resaltado (degradado azul con una raya a la izquierda) se desliza con `translateY` hasta la
    sección abierta.
  - El cuadrito del icono de la sección abierta se llena con el degradado azul a cian y crece un poco.
  - Verificar y Detalles conservan su tono verde azulado.
- **Cabecera de la sección.**
  - Título a 26 px, peso 700.
  - Cuadrito del icono con el degradado.
  - Un resplandor azul suave detrás, que no se mueve.
- **Lista vacía.**
  - La flecha de soltar flota despacio.
  - Un anillo late detrás de ella, con `scale` y `opacity`.
- **Lista con juegos.**
  - Cada juego que se añade entra subiendo y apareciendo. Solo al añadirse, no al repintar.
  - Las barras de cada juego se dibujan con `scaleX`: azul a cian al comprimir, menta al verificar,
    con brillo.
  - El estado lleva su color. Mientras trabaja, su punto parpadea.
  - El «−72 %» aparece con un salto al terminar.
- **Abajo.** La barra total es más gruesa, con degradado, brillo y halo. El botón Comprimir lleva el
  degradado azul.
- **Opciones.**
  - Las tarjetas de «Hecho para» se marcan con borde y fondo azul.
  - El punto de la opción elegida aparece con un salto.
  - El nivel se ve en grande.
- **Diálogos.** Entran creciendo de 0,94 a 1 y apareciendo. El fondo se funde. Los de aviso
  conservan su borde ámbar arriba.
- **El final de una compresión** (`done-dialog`, cuando la cola comprimió y se ahorró espacio):
  1. Se ve el tamaño de antes en grande: la suma de `OriginalSize` de los juegos que quedaron bien.
  2. Se aprieta en horizontal (`scaleX`) y desaparece, y sale el tamaño de ahora: la suma de
     `outSize`.
  3. A la vez, la barra llena se aprieta hasta la proporción nueva. Lo liberado queda a la derecha
     en menta rayada. Debajo pone «Antes: X» y «Ahora: Y».
  4. «X ahorrados» cuenta hacia arriba hasta `summary.Saved`, con un estallido de unos 18 puntos
     (`transform` y `opacity`).
  5. El icono de comprimir junta sus flechas.

  El resto del diálogo sigue igual: estadísticas, «Lo que más ahorró», «Y ahora» y el aviso de
  ampr-emu-ng. Al descomprimir o si no se ahorró nada, no hay barra ni cuenta, y el diálogo es el de
  siempre con su entrada animada. Con movimiento reducido se ven directamente los números finales.
- **Textos nuevos** (es.js y en.js): `doneBefore` («Antes: {tamaño}») y `doneAfter` («Ahora:
  {tamaño}»).
- **Tema claro.** Las mismas formas con la paleta clara de 5.1.

### 5.4 Rendimiento en la PS5

- Fuera el `backdrop-filter` de tarjetas, chips, píldoras, botones y avisos.
- Los degradados del fondo son radiales y no llevan `filter: blur`.
- Solo se animan `transform` y `opacity`, que la PS5 hace en la GPU.
- `will-change` solo en las dos capas del fondo.
- El brillo de las barras va en un pseudo-elemento que se traslada.
- Se comprueba en el simulador a 1920 × 1080 con el panel de rendimiento del navegador: ni tareas
  largas ni repintados de toda la pantalla mientras cambia el foco.

### 5.5 Accesibilidad

- **Foco visible siempre.** En las tarjetas, el anillo blanco. En lo demás, el contorno de ahora con
  el azul nuevo.
- **Contraste AA** para el texto sobre el fondo ambiente: el velo oscuro lo garantiza. Se comprueba
  con los colores más claros que puede dar un icono.
- **El nombre grande es `aria-hidden`.** El estallido y las capas de fondo no se anuncian.
- **Movimiento reducido** respetado en las dos interfaces.

### 5.6 Al construirlo

- **Este documento manda sobre la maqueta.** La maqueta (`body[data-dir="a"]`) es la referencia
  visual: su CSS se puede traer quitando lo que la PS5 no tiene (`aspect-ratio`, `:has()`, `inset`).
- **El desenfoque.** Se quitan todos los usos de `backdrop-filter` de la consola menos el del fondo
  de la ficha y de los diálogos (`.scrim`).
- **Las barras conservan su HTML.** La pista sigue siendo el elemento de fuera y el relleno el de
  dentro. El relleno pasa a ocupar todo el ancho (`width: 100%`), con `transform-origin` a la
  izquierda, y JS le pone `transform: scaleX(f)` en vez de `width`. En árabe, el origen pasa a la
  derecha.
- **Se pueden añadir ids y elementos.** Las pruebas exigen los que ya hay, no prohíben otros.
- **Los números del final.**
  - «Antes» es la suma de `OriginalSize` de los juegos que quedaron bien, y «Ahora» la suma de su
    `outSize`. La barra se aprieta hasta Ahora/Antes.
  - La cuenta termina en `summary.Saved`, el mismo número que dice el aviso del sistema.
  - Si falta algún tamaño o Antes es 0, no hay barra, y se ve solo el número ahorrado.
- **La caché sin conexión de la consola** (appcache) no se toca: el manifiesto lleva la versión y el
  hash de la página, y se renueva solo.

## 6. Lo que decidí yo (revísalo)

1. El nombre grande del juego enfocado en la consola, que es lo único nuevo que se ve.
2. Quitar casi todo el desenfoque de la consola, por rendimiento.
3. Los títulos de grupo en frase normal en vez de MAYÚSCULAS.
4. La paleta del tema claro del PC, que las maquetas no enseñaban.
5. Atenuar al 85 % las tarjetas sin foco mientras el foco está en la cuadrícula.
6. Los dos textos nuevos del PC (`doneBefore` y `doneAfter`).
7. Rehacer las capturas de `docs/img/ventana-*.jpg`, que salen en la guía, con el aspecto nuevo.

## 7. Lo que queda para quien lo construya

Los valores exactos de espaciado y sombras dentro de lo dicho, la forma del halo, el número de
puntos del estallido y cómo repartir el CSS en el archivo. Siempre que se mantenga lo de 3 y 5.

## 8. Cómo se comprueba

- **Consola:** `node tools/mock_ui.js --port 8797`, a 1920 × 1080 y a 390 × 844. Se revisan:
  - biblioteca con y sin juegos, filtros y unidades;
  - la ficha de un juego, con error y sin él;
  - Instalar con trabajo en curso, en pausa y terminado;
  - Ajustes y el diálogo de confirmar;
  - el foco con las flechas;
  - árabe (dirección de derecha a izquierda) y movimiento reducido.

  Después, `node --check` del script y de `tools/mock_ui.js`, y `bash tools/check_identity.sh`.
- **PC:** el simulador de la ventana, a 1180 × 720 y a 800 × 600, en tema oscuro y claro. Se revisan:
  - las cinco secciones;
  - una cola comprimiendo;
  - el final con la animación;
  - cada diálogo de aviso;
  - soltar archivos;
  - movimiento reducido.

  Después, `go test ./...` en `gui/`, que lanza las pruebas de la página.
- **Dónde corre cada simulador.** Los dos se abren en el navegador integrado, que es Chromium. El
  de la ventana del PC es un servidor pequeño que carga `gui/frontend` con un sustituto de la
  ventana (`window.go` y `window.runtime`). Con él se hacen también las capturas nuevas de
  `docs/img/ventana-*.jpg`, al mismo tamaño que las de ahora.
- **En tu PS5.** El WebKit de la consola solo se puede probar en ella. Cuando las ramas estén
  subidas, lo último es abrir la página en la PS5 y mover el cursor por la biblioteca: el fondo y
  las tarjetas tienen que cambiar sin tirones. Si hay tirones, se quita primero el atenuado de las
  demás tarjetas y luego el halo.
- **CI en verde** en las dos ramas, `ui-rediseno` (maxpack-ps5) e `interfaz-pc-rediseno` (maxpack),
  subidas a origin. Sin PR ni fusión: eso lo decides tú.
