const CONSOLE_MESSAGES = {
  INVALID_PARAM_LANGUAGE: (param) =>
    `Invalid parameter for \`language\` provided. Expected a string, but got ${typeof param}.`,
  INVALID_PARAM_JSON: (param) =>
    `Invalid parameter for \`json\` provided. Expected an object, but got ${typeof param}.`,
  EMPTY_PARAM_LANGUAGE: () =>
    `The parameter for \`language\` can't be an empty string.`,
  EMPTY_PARAM_JSON: () =>
    `The parameter for \`json\` must have at least one key/value pair.`,
  INVALID_PARAM_KEY: (param) =>
    `Invalid parameter for \`key\` provided. Expected a string, but got ${typeof param}.`,
  NO_LANGUAGE_REGISTERED: (language) =>
    `No translation for language "${language}" has been added, yet. Make sure to register that language using the \`.add()\` method first.`,
  TRANSLATION_NOT_FOUND: (key, language) =>
    `No translation found for key "${key}" in language "${language}". Is there a key/value in your translation file?`,
  INVALID_PARAMETER_SOURCES: (param) =>
    `Invalid parameter for \`sources\` provided. Expected either a string or an array, but got ${typeof param}.`,
  FETCH_ERROR: (response) =>
    `Could not fetch "${response.url}": ${response.status} (${response.statusText})`,
  INVALID_ENVIRONMENT: () =>
    `You are trying to execute the method \`translatePageTo()\`, which is only available in the browser. Your environment is most likely Node.js`,
  MODULE_NOT_FOUND: (message) => message,
  MISMATCHING_ATTRIBUTES: (keys, attributes, element) =>
    `The attributes "data-i18n" and "data-i18n-attr" must contain the same number of keys.

Values in \`data-i18n\`:      (${keys.length}) \`${keys.join(' ')}\`
Values in \`data-i18n-attr\`: (${attributes.length}) \`${attributes.join(' ')}\`

The HTML element is:
${element.outerHTML}`,
  INVALID_OPTIONS: (param) =>
    `Invalid config passed to the \`Translator\` constructor. Expected an object, but got ${typeof param}. Using default config instead.`,
};

/**
 *
 * @param {Boolean} isEnabled
 * @return {Function}
 */
export function logger(isEnabled) {
  return function log(code, ...args) {
    if (isEnabled) {
      try {
        const message = CONSOLE_MESSAGES[code];
        throw new TypeError(message ? message(...args) : 'Unhandled Error');
      } catch (ex) {
        const line = ex.stack.split(/\n/g)[1];
        const [method, filepath] = line.split(/@/);

        console.error(`${ex.message}

This error happened in the method \`${method}\` from: \`${filepath}\`.

If you don't want to see these error messages, turn off debugging by passing \`{ debug: false }\` to the constructor.

Error code: ${code}

Check out the documentation for more details about the API:
https://github.com/andreasremdt/simple-translator#usage
        `);
      }
    }
  };
}
