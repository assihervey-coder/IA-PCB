// Config PostCSS locale au frontend YahriaCad.
// Essentielle : sans elle, Next.js remonte l'arborescence jusqu'au
// postcss.config.mjs de la racine du dépôt (template Tailwind v4 avec
// @tailwindcss/postcss) et le build échoue en CI où ce paquet n'est pas
// installé ici. Le frontend n'utilise que Sass (globals.scss) : aucun
// plugin PostCSS n'est nécessaire.
const config = {
  plugins: {},
};

export default config;
