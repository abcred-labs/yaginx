import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  tutorialSidebar: [
    'intro',
    'help-system',
    'getting-started',
    'configuration',
    'proxy-and-certificates',
    'migrations',
    'monitoring',
    {
      type: 'category',
      label: 'Operations',
      items: ['operations/maintenance', 'operations/troubleshooting'],
    },
    {
      type: 'category',
      label: 'Reference',
      items: ['reference/api', 'release-readiness'],
    },
    'security',
    'contributing',
  ],
};

export default sidebars;
