import type { Status } from '../../types';
import { el, setText, setValue } from '../dom';

/** The folder the data file paths share: the root certificate's path without its last segment. */
export const dataFolder = (status: Status | null): string => status?.ca.path.replace(/[\\/][^\\/]*$/, '') ?? '';

/** Preferences > General > Settings Storage: where the server keeps its files (the root certificate and key, the token),
 *  read from the status, and where the window's own preferences live. */
export class StorageSection {
  readonly el = el('<div class="cv-panel-section cv-settings-location"></div>');
  private readonly path = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Data folder" spellcheck="false">');
  private readonly note = el('<p class="cv-note" role="status"></p>');

  constructor() {
    const row = el('<label class="cv-dialog-row"><span>Data folder</span></label>');
    row.appendChild(this.path);
    this.el.append(row, this.note);
    this.refresh(null);
  }

  refresh(status: Status | null): void {
    const folder = dataFolder(status);
    setValue(this.path, folder);
    setText(this.note, folder
      ? 'The root certificate, its key and this run\'s token live there (FORMNET_DATA chooses it). The HTTPS choice and the passthrough list, set here or in Setup, are kept when FormNET restarts; the other server settings come from FORMNET_* variables, listed in the README. The window\'s own preferences (this dialog, the theme) stay in this browser profile.'
      : 'The data folder is shown once the server has answered.');
  }
}
