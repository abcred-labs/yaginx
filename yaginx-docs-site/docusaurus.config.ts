import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Yaginx',
  tagline: 'An open-source HTTP/HTTPS gateway and management plane',
  favicon: 'img/logo.svg',
  url: 'https://abcred-labs.github.io',
  baseUrl: '/yaginx/',
  organizationName: 'abcred-labs',
  projectName: 'yaginx',
  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'throw',
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },
  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          editUrl:
            'https://github.com/abcred-labs/yaginx/tree/main/yaginx-docs-site',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],
  themeConfig: {
    image: 'img/logo.svg',
    navbar: {
      title: 'Yaginx',
      logo: {
        alt: 'Yaginx logo',
        src: 'img/logo.svg',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'tutorialSidebar',
          position: 'left',
          label: 'Documentation',
        },
        {
          href: 'https://github.com/abcred-labs/yaginx/releases',
          label: 'Releases',
          position: 'right',
        },
        {
          href: 'https://github.com/abcred-labs/yaginx',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Documentation',
          items: [
            {label: 'Getting started', to: '/docs/intro'},
            {label: 'Operations', to: '/docs/operations/maintenance'},
            {label: 'API and MCP', to: '/docs/reference/api'},
          ],
        },
        {
          title: 'Project',
          items: [
            {
              label: 'Releases',
              href: 'https://github.com/abcred-labs/yaginx/releases',
            },
            {
              label: 'Issues',
              href: 'https://github.com/abcred-labs/yaginx/issues',
            },
            {
              label: 'Security policy',
              href: 'https://github.com/abcred-labs/yaginx/blob/main/SECURITY.md',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Yaginx contributors. Licensed under GPL-3.0.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
