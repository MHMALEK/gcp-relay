import functions_framework


@functions_framework.cloud_event
def hello(cloud_event):
    """Triggered by a Cloud Storage object event delivered as a CloudEvent."""
    data = cloud_event.data
    print(
        f"gcp-relay: received {cloud_event['type']} "
        f"for gs://{data.get('bucket')}/{data.get('name')} "
        f"(size={data.get('size')})"
    )
