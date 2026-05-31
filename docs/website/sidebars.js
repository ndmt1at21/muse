// @ts-check
// Manual sidebar so the reading order tells a story: what it is → how it's built
// → how things flow → how to use it → reference.

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docs: [
    'intro',
    {
      type: 'category',
      label: 'Architecture',
      collapsed: false,
      items: [
        'architecture/overview',
        'architecture/topology',
        'architecture/tenancy-identity',
        'architecture/data-model',
      ],
    },
    {
      type: 'category',
      label: 'Concepts',
      collapsed: false,
      items: [
        'concepts/generic-engine',
        'concepts/anti-cheat',
        'concepts/rewards-fulfillment',
        'concepts/wallet-milestones',
        'concepts/quests-leaderboard',
        'concepts/integration-hub',
      ],
    },
    {
      type: 'category',
      label: 'Flows',
      collapsed: false,
      items: [
        'flows/gameplay',
        'flows/player-auth',
        'flows/rewards-fulfillment',
        'flows/wallet-redeem',
        'flows/leaderboard',
        'flows/integration-events',
      ],
    },
    {
      type: 'category',
      label: 'Guides',
      collapsed: false,
      items: [
        'guides/quickstart',
        'guides/add-a-game',
        'guides/add-a-shape',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      collapsed: false,
      items: [
        'reference/rest-api',
        'reference/errors',
        'reference/observability',
      ],
    },
  ],
};

module.exports = sidebars;
