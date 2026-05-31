// @ts-check
// Docusaurus v3 configuration for the Muse documentation site.
// Mermaid is enabled (markdown.mermaid + theme-mermaid) so architecture and flow
// diagrams render from fenced ```mermaid code blocks.
const { themes } = require('prism-react-renderer');

// On GitHub Pages the deploy workflow injects DOCS_URL (origin) and DOCS_BASE_URL
// (the project base path, e.g. "/muse"). Locally these are unset → root at "/".
// baseUrl must start and end with "/", so we normalize the trailing slash.
const baseUrl = (process.env.DOCS_BASE_URL || '/').replace(/\/?$/, '/');

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Muse',
  tagline: 'A config-driven game service — add a game, not backend code.',
  favicon: 'img/favicon.svg',

  url: process.env.DOCS_URL || 'https://example.github.io',
  baseUrl,

  organizationName: 'muse',
  projectName: 'muse',

  onBrokenLinks: 'warn',

  i18n: { defaultLocale: 'en', locales: ['en'] },

  markdown: {
    mermaid: true,
    hooks: { onBrokenMarkdownLinks: 'warn' },
  },
  themes: ['@docusaurus/theme-mermaid'],

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          routeBasePath: '/', // docs are the site root
          sidebarPath: require.resolve('./sidebars.js'),
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      colorMode: { respectPrefersColorScheme: true },
      navbar: {
        title: 'Muse',
        items: [
          { type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs' },
          { to: '/architecture/overview', label: 'Architecture', position: 'left' },
          { to: '/flows/gameplay', label: 'Flows', position: 'left' },
          { to: '/guides/quickstart', label: 'Quickstart', position: 'left' },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Docs',
            items: [
              { label: 'Introduction', to: '/' },
              { label: 'Architecture', to: '/architecture/overview' },
              { label: 'Flows', to: '/flows/gameplay' },
            ],
          },
          {
            title: 'Build',
            items: [
              { label: 'Quickstart', to: '/guides/quickstart' },
              { label: 'Add a game', to: '/guides/add-a-game' },
              { label: 'REST API', to: '/reference/rest-api' },
            ],
          },
        ],
        copyright: 'Muse — config-driven game service.',
      },
      prism: {
        theme: themes.github,
        darkTheme: themes.dracula,
        additionalLanguages: ['bash', 'json', 'protobuf', 'go'],
      },
      mermaid: {
        theme: { light: 'neutral', dark: 'dark' },
      },
    }),
};

module.exports = config;
