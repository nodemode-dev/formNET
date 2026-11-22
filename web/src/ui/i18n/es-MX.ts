/** Mexican Spanish: only what Latin American Spanish says differently from the Spanish table, which it falls back to
 *  for everything else (ui/i18n/langs.ts, LANG_PARENT). The first two are formvis's own (Apache-2.0); "agregar" for adding
 *  and "presione" for pressing a key are formNET's. */
export const esMX: Record<string, string> = {
  "Clear": "Borrar",
  "Paste": "Pegar",

  "Add {host} to passthrough": "Agregar {host} al paso directo",
  "Added {host} to passthrough: FormNET tunnels it from the next connection and only counts it.": "{host} se agregó al paso directo: FormNET lo pasa por un túnel desde la próxima conexión y solo lo cuenta.",
  "Filter flows (press / )": "Filtrar flujos (presione /)",
  "No command is known for this system: add ca.pem to its trusted certificates with the system's own tool.": "No se conoce ningún comando para este sistema: agregue ca.pem a sus certificados de confianza con la herramienta propia del sistema.",
  "This system has no command FormNET knows: add ca.pem to its trusted certificates with the system's own tool.": "FormNET no conoce ningún comando para este sistema: agregue ca.pem a sus certificados de confianza con la herramienta propia del sistema.",

  "Find text in bodies (press / )": "Buscar texto en los cuerpos (presione /)",
  "Only audio and video": "Solo audio y video",
  "Searching the captured request and response bodies for the text; press to search URLs again": "Se busca el texto en los cuerpos capturados de solicitudes y respuestas; presione para volver a buscar en las URL",
  "The requests in the HAR are added to the recording, marked as from a session; nothing is replaced. Bodies longer than the body limit are cut, and the oldest flows go when the recording is full.": "Las solicitudes del HAR se agregan a la grabación, marcadas como procedentes de una sesión; no se reemplaza nada. Los cuerpos más largos que el límite de cuerpo se cortan, y los flujos más antiguos salen cuando la grabación está llena.",
  "Authorization, Proxy-Authorization, Cookie and Set-Cookie values are left out, so requests that need them will be refused until you add them back.": "Los valores de Authorization, Proxy-Authorization, Cookie y Set-Cookie se omiten, así que las solicitudes que los necesiten serán rechazadas hasta que los vuelva a agregar.",
};
