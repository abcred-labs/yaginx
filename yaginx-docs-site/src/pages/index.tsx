import clsx from 'clsx';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';
import Heading from '@theme/Heading';
import styles from './index.module.css';

function HomepageHeader() {
  return (
    <header className={clsx('hero hero--primary', styles.heroBanner)}>
      <div className="container">
        <Heading as="h1" className="hero__title">
          Yaginx
        </Heading>
        <p className="hero__subtitle">
          An open-source HTTP/HTTPS gateway with a PostgreSQL-backed management
          plane.
        </p>
        <div className={styles.buttons}>
          <Link className="button button--secondary button--lg" to="/docs/intro">
            Read the documentation
          </Link>
          <a className="button button--outline button--lg" href="https://github.com/abcred-labs/yaginx/releases">
            View releases
          </a>
        </div>
      </div>
    </header>
  );
}

export default function Home(): JSX.Element {
  return (
    <Layout title="Yaginx documentation" description="Yaginx gateway documentation">
      <HomepageHeader />
      <main>
        <section className="container padding-vert--lg">
          <div className="row">
            <div className="col col--4">
              <Heading as="h2">Production gateway</Heading>
              <p>
                Route HTTP, HTTPS, HTTP/3, WebSocket and streaming traffic with
                host/path rules, transforms, CORS and ACME certificates.
              </p>
            </div>
            <div className="col col--4">
              <Heading as="h2">Operational control</Heading>
              <p>
                Manage websites, domains, nodes, credentials, request history,
                analytics and runtime settings from the React console.
              </p>
            </div>
            <div className="col col--4">
              <Heading as="h2">Safe upgrades</Heading>
              <p>
                Use PostgreSQL migrations, health-gated package upgrades,
                last-known-good snapshots and a database-free rescue runtime.
              </p>
            </div>
          </div>
        </section>
      </main>
    </Layout>
  );
}
