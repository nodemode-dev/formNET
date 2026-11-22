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
};
