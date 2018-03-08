# cacher
Global resources caching infrastructure with AWS S3 for storage
and Google Cloud Spanner for the DB.

A centralized file download cacher that stores resources for
distributed readers using Cloud Spanner for global availability.

It can be deployed as a backend web app on your laptop
or any cloud provider. Make requests as a pass-through "proxy"
for your web resources.

### Problem
Fetching some resources outside of your CDN can be expensive
e.g. if you are processing billions of image assets for your
customers within a network and would prefer centralized
resources, or crawling the web at the end of everyday.

Due to the intensivity and contention, and deployment of
processing on AWS EC2, we need to store resources on AWS S3.
However the workers process on Google Cloud Platform plus
Cloud Spanner is super fast and globally available so
using it as the DB.

### Operation
When a user requests for a URL through cacher, it
first checked locally and if present, it is served, otherwise
it'll be downloaded while being proxied back.

### Uses
* CORS and mixed content resource fixing for your own assets
* Edge caching say at the end of the day, refresh your resources
once but where the work is done by multitudes of distributed workers
* Web crawlers at the end of everyday and caching resources
within your CDN to cut out expensively getting out of the network
* A browser extension for vetting resources e.g. safe for browsing

### Sample usage
```shell
$ curl -X POST http://localhost:9444 --data '{"url":"https://orijtech.com/images/logoCenter.png"}'
```

```JSON
{
  "original_url":"https://orijtech.com/images/logoCenter.png",
  "cached_url":"https://cacher-app.s3.amazonaws.com/orijtech.com/adeee3db23c8eb5373aa2675fe2f8394",
  "time_at":1520504398
}
```
