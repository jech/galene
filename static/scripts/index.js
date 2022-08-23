// The below provided options are default.
var translator = new Translator({
  defaultLanguage: "en",
  detectLanguage: true,
  selector: "[data-i18n]",
  debug: false,
  registerGlobally: "__",
  persist: false,
  persistKey: "preferred_language",
  filesLocation: "/lang"
});

translator.fetch(["en", "oc", "fr"]).then(() => {
  // Calling `translatePageTo()` without any parameters
  // will translate to the default language.
  translator.translatePageTo();
  registerLanguageToggle();
});

function registerLanguageToggle() {
  var select = document.querySelector("select");

  select.addEventListener("change", evt => {
    var language = evt.target.value;
    translator.translatePageTo(language);
  });
};
document.getElementById(translator.currentLanguage).selected = true; 

