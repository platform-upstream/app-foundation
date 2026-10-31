import { createJSONClient } from '../foundation/api.js';

// Same-origin JSON client. An expired Cloudflare Access session answers API calls with a redirect;
// reloading the page sends the browser through the Access sign-in and back.
export const request = createJSONClient({
  fallbackMessage: 'The action could not be completed.',
  onAuthRequired: () => location.reload(),
});
